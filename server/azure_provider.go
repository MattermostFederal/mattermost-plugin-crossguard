package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azqueue"
	"github.com/mattermost/mattermost/server/public/plugin"
)

const (
	// azureMaxMessageSize is the safe message size limit before Base64 encoding to 64KB.
	azureMaxMessageSize = 48000

	azureQueuePollInterval = 5 * time.Second
	azureBlobPollInterval  = 15 * time.Second
	azureVisibilityTimeout = 5 * time.Minute
	azureDequeueBatchSize  = int32(32)

	azureErrQueueAlreadyExists     = "QueueAlreadyExists"
	azureErrContainerAlreadyExists = "ContainerAlreadyExists"
	blobMetadataHeadersKey         = "crossguard_headers"
)

// azureProvider implements QueueProvider using Azure Queue Storage and Azure Blob Storage.
type azureProvider struct {
	queueClient     *azqueue.QueueClient
	containerClient *container.Client
	api             plugin.API
	cfg             AzureProviderConfig
	cancel          context.CancelFunc
	handler         func(data []byte) error
	pollDone        chan struct{}
}

func newAzureProvider(cfg AzureProviderConfig, api plugin.API) (QueueProvider, error) {
	queueClient, err := azqueue.NewQueueClientFromConnectionString(cfg.ConnectionString, cfg.QueueName, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure Queue client: %w", err)
	}

	// Ensure the queue exists (idempotent, returns success if already created).
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, createErr := queueClient.Create(ctx, nil); createErr != nil {
		// Ignore "already exists" (409 Conflict); warn on other errors.
		if !strings.Contains(createErr.Error(), azureErrQueueAlreadyExists) {
			api.LogWarn("Azure Queue: could not create queue (may already exist)", "queue", cfg.QueueName, "error", createErr.Error())
		}
	}

	var containerClient *container.Client
	if cfg.BlobContainerName != "" {
		containerClient, err = container.NewClientFromConnectionString(cfg.ConnectionString, cfg.BlobContainerName, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create Azure Blob container client: %w", err)
		}

		// Ensure the blob container exists (fresh timeout, independent of queue creation).
		blobCtx, blobCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer blobCancel()
		if _, createErr := containerClient.Create(blobCtx, nil); createErr != nil {
			if !strings.Contains(createErr.Error(), azureErrContainerAlreadyExists) {
				api.LogWarn("Azure Blob: could not create container (may already exist)", "container", cfg.BlobContainerName, "error", createErr.Error())
			}
		}
	}

	return &azureProvider{
		queueClient:     queueClient,
		containerClient: containerClient,
		api:             api,
		cfg:             cfg,
	}, nil
}

func (a *azureProvider) Publish(ctx context.Context, data []byte) error {
	encoded := base64.StdEncoding.EncodeToString(data)
	_, err := a.queueClient.EnqueueMessage(ctx, encoded, nil)
	if err != nil {
		return fmt.Errorf("failed to enqueue message: %w", err)
	}
	return nil
}

func (a *azureProvider) Subscribe(ctx context.Context, handler func(data []byte) error) error {
	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	a.handler = handler
	a.pollDone = make(chan struct{})

	go func() {
		defer close(a.pollDone)
		a.pollQueue(ctx)
	}()
	return nil
}

func (a *azureProvider) pollQueue(ctx context.Context) {
	visTimeout := int32(azureVisibilityTimeout.Seconds())
	batchSize := azureDequeueBatchSize
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		resp, err := a.queueClient.DequeueMessages(ctx, &azqueue.DequeueMessagesOptions{
			NumberOfMessages:  &batchSize,
			VisibilityTimeout: &visTimeout,
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			a.api.LogError("Azure Queue dequeue failed", "queue", a.cfg.QueueName, "error", err.Error())
			select {
			case <-ctx.Done():
				return
			case <-time.After(azureQueuePollInterval):
			}
			continue
		}

		if len(resp.Messages) == 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(azureQueuePollInterval):
			}
			continue
		}

		for _, msg := range resp.Messages {
			if msg.MessageText == nil || msg.MessageID == nil || msg.PopReceipt == nil {
				continue
			}

			data, err := base64.StdEncoding.DecodeString(*msg.MessageText)
			if err != nil {
				a.api.LogError("Azure Queue: failed to decode message",
					"queue", a.cfg.QueueName, "error", err.Error())
				// Delete malformed message to avoid reprocessing.
				_, _ = a.queueClient.DeleteMessage(ctx, *msg.MessageID, *msg.PopReceipt, nil)
				continue
			}

			if err := a.handler(data); err != nil {
				a.api.LogWarn("Azure Queue: handler returned error, message will retry",
					"queue", a.cfg.QueueName, "error", err.Error())
				continue
			}

			// Handler succeeded, delete the message.
			if _, err := a.queueClient.DeleteMessage(ctx, *msg.MessageID, *msg.PopReceipt, nil); err != nil {
				a.api.LogWarn("Azure Queue: failed to delete processed message",
					"queue", a.cfg.QueueName, "error", err.Error())
			}
		}
	}
}

// azureBlobMetadata is stored as JSON in blob metadata for header transport.
type azureBlobMetadata struct {
	Headers map[string]string `json:"headers"`
}

func (a *azureProvider) UploadFile(ctx context.Context, key string, data []byte, headers map[string]string) error {
	if a.containerClient == nil {
		return fmt.Errorf("blob container not configured")
	}

	blobClient := a.containerClient.NewBlockBlobClient(key)

	meta := azureBlobMetadata{Headers: headers}
	metaJSON, _ := json.Marshal(meta)

	encoded := base64.StdEncoding.EncodeToString(metaJSON)
	blobMeta := map[string]*string{
		blobMetadataHeadersKey: &encoded,
	}

	_, err := blobClient.Upload(ctx, newNopCloser(data), &blockblob.UploadOptions{
		Metadata: blobMeta,
	})
	if err != nil {
		return fmt.Errorf("failed to upload blob %q: %w", key, err)
	}
	return nil
}

func (a *azureProvider) WatchFiles(ctx context.Context, handler func(key string, data []byte, headers map[string]string) error) error {
	if a.containerClient == nil {
		return nil
	}

	var lastMarker string
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(azureBlobPollInterval):
		}

		pager := a.containerClient.NewListBlobsFlatPager(&container.ListBlobsFlatOptions{
			Marker:  optStr(lastMarker),
			Include: container.ListBlobsInclude{Metadata: true},
		})

		for pager.More() {
			resp, err := pager.NextPage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				a.api.LogError("Azure Blob list failed", "container", a.cfg.BlobContainerName, "error", err.Error())
				break
			}

			for _, blob := range resp.Segment.BlobItems {
				if blob.Name == nil {
					continue
				}

				blobClient := a.containerClient.NewBlockBlobClient(*blob.Name)

				headers := extractBlobHeaders(blob.Metadata)

				// Download blob data.
				downloadResp, err := blobClient.DownloadStream(ctx, nil)
				if err != nil {
					a.api.LogWarn("Azure Blob: failed to download",
						"blob", *blob.Name, "error", err.Error())
					continue
				}

				data, err := io.ReadAll(downloadResp.Body)
				_ = downloadResp.Body.Close()
				if err != nil {
					a.api.LogWarn("Azure Blob: failed to read download",
						"blob", *blob.Name, "error", err.Error())
					continue
				}

				if err := handler(*blob.Name, data, headers); err != nil {
					a.api.LogWarn("Azure Blob: handler returned error",
						"blob", *blob.Name, "error", err.Error())
					continue
				}

				// Delete blob after successful processing. Only advance the
				// marker if the delete succeeds so the blob is re-listed on
				// the next poll cycle when deletion fails.
				if _, err := blobClient.Delete(ctx, nil); err != nil {
					a.api.LogWarn("Azure Blob: failed to delete after processing",
						"blob", *blob.Name, "error", err.Error())
					continue
				}

				lastMarker = *blob.Name
			}
		}
	}
}

func (a *azureProvider) MaxMessageSize() int {
	return azureMaxMessageSize
}

func (a *azureProvider) Close() error {
	if a.cancel != nil {
		a.cancel()
	}
	if a.pollDone != nil {
		<-a.pollDone
	}
	return nil
}

// testAzureConnection tests connectivity to Azure Queue Storage by sending and receiving a test message.
func testAzureConnection(cfg AzureProviderConfig) error {
	queueClient, err := azqueue.NewQueueClientFromConnectionString(cfg.ConnectionString, cfg.QueueName, nil)
	if err != nil {
		return fmt.Errorf("failed to create queue client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Ensure the queue exists before testing.
	if _, createErr := queueClient.Create(ctx, nil); createErr != nil {
		if !strings.Contains(createErr.Error(), azureErrQueueAlreadyExists) {
			return fmt.Errorf("failed to create queue: %w", createErr)
		}
	}

	testMsg := base64.StdEncoding.EncodeToString([]byte("crossguard-test-" + time.Now().Format(time.RFC3339)))
	enqResp, err := queueClient.EnqueueMessage(ctx, testMsg, nil)
	if err != nil {
		return fmt.Errorf("failed to enqueue test message: %w", err)
	}

	if len(enqResp.Messages) > 0 && enqResp.Messages[0].MessageID != nil && enqResp.Messages[0].PopReceipt != nil {
		_, _ = queueClient.DeleteMessage(ctx, *enqResp.Messages[0].MessageID, *enqResp.Messages[0].PopReceipt, nil)
	}

	return nil
}

func extractBlobHeaders(metadata map[string]*string) map[string]string {
	headers := make(map[string]string)
	if metadata == nil {
		return headers
	}

	raw, ok := metadata[blobMetadataHeadersKey]
	if !ok || raw == nil {
		return headers
	}

	decoded, err := base64.StdEncoding.DecodeString(*raw)
	if err != nil {
		return headers
	}

	var meta azureBlobMetadata
	if err := json.Unmarshal(decoded, &meta); err != nil {
		return headers
	}

	return meta.Headers
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

type nopCloser struct {
	*bytes.Reader
}

func (nopCloser) Close() error { return nil }

func newNopCloser(data []byte) io.ReadSeekCloser {
	return nopCloser{bytes.NewReader(data)}
}
