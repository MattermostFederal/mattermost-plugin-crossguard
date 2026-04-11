package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azqueue"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAzureCloseWithoutSubscribe(t *testing.T) {
	p := &azureProvider{}
	assert.NoError(t, p.Close())
}

func TestAzureUploadFileWithoutBlobClient(t *testing.T) {
	p := &azureProvider{containerClient: nil}
	err := p.UploadFile(context.TODO(), "key", []byte("data"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blob container not configured")
}

func TestAzureWatchFilesWithoutBlobClient(t *testing.T) {
	p := &azureProvider{containerClient: nil}
	err := p.WatchFiles(context.TODO(), nil)
	assert.NoError(t, err)
}

func TestExtractBlobHeaders(t *testing.T) {
	t.Run("nil metadata returns empty map", func(t *testing.T) {
		headers := extractBlobHeaders(nil)
		assert.Empty(t, headers)
	})

	t.Run("missing crossguard_headers key returns empty map", func(t *testing.T) {
		v := "something"
		headers := extractBlobHeaders(map[string]*string{"other": &v})
		assert.Empty(t, headers)
	})

	t.Run("nil value for key returns empty map", func(t *testing.T) {
		headers := extractBlobHeaders(map[string]*string{"crossguard_headers": nil})
		assert.Empty(t, headers)
	})

	t.Run("invalid base64 returns empty map", func(t *testing.T) {
		v := "not-valid-base64!!!"
		headers := extractBlobHeaders(map[string]*string{"crossguard_headers": &v})
		assert.Empty(t, headers)
	})

	t.Run("invalid JSON returns empty map", func(t *testing.T) {
		v := base64.StdEncoding.EncodeToString([]byte("not json"))
		headers := extractBlobHeaders(map[string]*string{"crossguard_headers": &v})
		assert.Empty(t, headers)
	})

	t.Run("valid metadata round-trips correctly", func(t *testing.T) {
		original := map[string]string{
			"X-Post-Id":   "post123",
			"X-Conn-Name": "my-conn",
			"X-Filename":  "report.pdf",
		}
		meta := azureBlobMetadata{Headers: original}
		metaJSON, err := json.Marshal(meta)
		require.NoError(t, err)

		encoded := base64.StdEncoding.EncodeToString(metaJSON)
		headers := extractBlobHeaders(map[string]*string{"crossguard_headers": &encoded})

		assert.Equal(t, original, headers)
	})
}

func TestOptStr(t *testing.T) {
	t.Run("empty string returns nil", func(t *testing.T) {
		assert.Nil(t, optStr(""))
	})

	t.Run("non-empty string returns pointer", func(t *testing.T) {
		result := optStr("hello")
		require.NotNil(t, result)
		assert.Equal(t, "hello", *result)
	})
}

func TestNewNopCloser(t *testing.T) {
	data := []byte("test data")
	rc := newNopCloser(data)

	buf := make([]byte, len(data))
	n, err := rc.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, len(data), n)
	assert.Equal(t, data, buf)

	assert.NoError(t, rc.Close())
}

// Compile-time interface conformance checks.
var (
	_ QueueProvider = (*azureProvider)(nil)
	_ QueueProvider = (*natsProvider)(nil)
)

func TestProviderMaxMessageSize(t *testing.T) {
	t.Run("azure returns 48000", func(t *testing.T) {
		p := &azureProvider{}
		assert.Equal(t, 48000, p.MaxMessageSize())
	})

	t.Run("nats returns 0 (no limit)", func(t *testing.T) {
		p := &natsProvider{}
		assert.Equal(t, 0, p.MaxMessageSize())
	})
}

func TestAzureCloseIdempotent(t *testing.T) {
	p := &azureProvider{}
	assert.NoError(t, p.Close())
	assert.NoError(t, p.Close())
}

func TestAzureCloseNilFields(t *testing.T) {
	p := &azureProvider{
		cancel:   nil,
		pollDone: nil,
	}
	assert.NoError(t, p.Close())
}

func TestExtractBlobHeaders_EmptyHeadersMap(t *testing.T) {
	emptyHeaders := map[string]string{}
	meta := azureBlobMetadata{Headers: emptyHeaders}
	metaJSON, err := json.Marshal(meta)
	require.NoError(t, err)
	encoded := base64.StdEncoding.EncodeToString(metaJSON)
	headers := extractBlobHeaders(map[string]*string{"crossguard_headers": &encoded})
	assert.Empty(t, headers)
}

// mockAzureQueue implements azureQueuer for unit tests.
type mockAzureQueue struct {
	enqueueFn func(ctx context.Context, content string, o *azqueue.EnqueueMessageOptions) (azqueue.EnqueueMessagesResponse, error)
	dequeueFn func(ctx context.Context, o *azqueue.DequeueMessagesOptions) (azqueue.DequeueMessagesResponse, error)
	deleteFn  func(ctx context.Context, messageID string, popReceipt string, o *azqueue.DeleteMessageOptions) (azqueue.DeleteMessageResponse, error)
}

func (m *mockAzureQueue) EnqueueMessage(ctx context.Context, content string, o *azqueue.EnqueueMessageOptions) (azqueue.EnqueueMessagesResponse, error) {
	if m.enqueueFn != nil {
		return m.enqueueFn(ctx, content, o)
	}
	return azqueue.EnqueueMessagesResponse{}, nil
}

func (m *mockAzureQueue) DequeueMessages(ctx context.Context, o *azqueue.DequeueMessagesOptions) (azqueue.DequeueMessagesResponse, error) {
	if m.dequeueFn != nil {
		return m.dequeueFn(ctx, o)
	}
	return azqueue.DequeueMessagesResponse{}, nil
}

func (m *mockAzureQueue) DeleteMessage(ctx context.Context, messageID string, popReceipt string, o *azqueue.DeleteMessageOptions) (azqueue.DeleteMessageResponse, error) {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, messageID, popReceipt, o)
	}
	return azqueue.DeleteMessageResponse{}, nil
}

func newTestAPI() *plugintest.API {
	api := &plugintest.API{}
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	return api
}

func TestAzurePublish_EncodesBase64(t *testing.T) {
	var captured string
	mq := &mockAzureQueue{
		enqueueFn: func(_ context.Context, content string, _ *azqueue.EnqueueMessageOptions) (azqueue.EnqueueMessagesResponse, error) {
			captured = content
			return azqueue.EnqueueMessagesResponse{}, nil
		},
	}

	p := &azureProvider{
		queueClient: mq,
		api:         newTestAPI(),
	}

	original := []byte("hello world")
	err := p.Publish(context.Background(), original)
	require.NoError(t, err)

	expected := base64.StdEncoding.EncodeToString(original)
	assert.Equal(t, expected, captured)

	decoded, err := base64.StdEncoding.DecodeString(captured)
	require.NoError(t, err)
	assert.Equal(t, original, decoded)
}

func TestAzurePublish_EnqueueError(t *testing.T) {
	mq := &mockAzureQueue{
		enqueueFn: func(_ context.Context, _ string, _ *azqueue.EnqueueMessageOptions) (azqueue.EnqueueMessagesResponse, error) {
			return azqueue.EnqueueMessagesResponse{}, fmt.Errorf("connection refused")
		},
	}

	p := &azureProvider{
		queueClient: mq,
		api:         newTestAPI(),
	}

	err := p.Publish(context.Background(), []byte("data"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to enqueue message")
	assert.Contains(t, err.Error(), "connection refused")
}

func TestAzurePollQueue_ContextCancellation(t *testing.T) {
	mq := &mockAzureQueue{
		dequeueFn: func(ctx context.Context, _ *azqueue.DequeueMessagesOptions) (azqueue.DequeueMessagesResponse, error) {
			return azqueue.DequeueMessagesResponse{}, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &azureProvider{
		queueClient: mq,
		api:         newTestAPI(),
		cfg:         AzureQueueProviderConfig{QueueName: "test-q"},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.pollQueue(ctx)
	}()

	cancel()
	select {
	case <-done:
		// pollQueue exited as expected.
	case <-time.After(5 * time.Second):
		t.Fatal("pollQueue did not exit after context cancellation")
	}
}

func TestAzurePollQueue_DequeueError_Continues(t *testing.T) {
	var callCount int64

	ctx, cancel := context.WithCancel(context.Background())

	mq := &mockAzureQueue{
		dequeueFn: func(_ context.Context, _ *azqueue.DequeueMessagesOptions) (azqueue.DequeueMessagesResponse, error) {
			count := atomic.AddInt64(&callCount, 1)
			if count >= 2 {
				cancel()
			}
			return azqueue.DequeueMessagesResponse{}, fmt.Errorf("transient error")
		},
	}

	p := &azureProvider{
		queueClient: mq,
		api:         newTestAPI(),
		cfg:         AzureQueueProviderConfig{QueueName: "test-q"},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.pollQueue(ctx)
	}()

	select {
	case <-done:
		assert.GreaterOrEqual(t, atomic.LoadInt64(&callCount), int64(2))
	case <-time.After(15 * time.Second):
		t.Fatal("pollQueue did not exit in time")
	}
}

func TestAzurePollQueue_MalformedBase64_DeletesMessage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var deletedID string
	var deletedReceipt string
	callNum := int64(0)

	mq := &mockAzureQueue{
		dequeueFn: func(_ context.Context, _ *azqueue.DequeueMessagesOptions) (azqueue.DequeueMessagesResponse, error) {
			n := atomic.AddInt64(&callNum, 1)
			if n == 1 {
				return azqueue.DequeueMessagesResponse{
					Messages: []*azqueue.DequeuedMessage{
						{
							MessageText: new("!!!not-base64!!!"),
							MessageID:   new("msg-1"),
							PopReceipt:  new("receipt-1"),
						},
					},
				}, nil
			}
			cancel()
			return azqueue.DequeueMessagesResponse{}, nil
		},
		deleteFn: func(_ context.Context, messageID string, popReceipt string, _ *azqueue.DeleteMessageOptions) (azqueue.DeleteMessageResponse, error) {
			deletedID = messageID
			deletedReceipt = popReceipt
			return azqueue.DeleteMessageResponse{}, nil
		},
	}

	p := &azureProvider{
		queueClient: mq,
		api:         newTestAPI(),
		cfg:         AzureQueueProviderConfig{QueueName: "test-q"},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.pollQueue(ctx)
	}()

	select {
	case <-done:
		assert.Equal(t, "msg-1", deletedID)
		assert.Equal(t, "receipt-1", deletedReceipt)
	case <-time.After(10 * time.Second):
		t.Fatal("pollQueue did not exit in time")
	}
}

func TestAzurePollQueue_HandlerError_DoesNotDelete(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var deleteCallCount int64
	callNum := int64(0)
	payload := base64.StdEncoding.EncodeToString([]byte("valid payload"))

	mq := &mockAzureQueue{
		dequeueFn: func(_ context.Context, _ *azqueue.DequeueMessagesOptions) (azqueue.DequeueMessagesResponse, error) {
			n := atomic.AddInt64(&callNum, 1)
			if n == 1 {
				return azqueue.DequeueMessagesResponse{
					Messages: []*azqueue.DequeuedMessage{
						{
							MessageText: &payload,
							MessageID:   new("msg-2"),
							PopReceipt:  new("receipt-2"),
						},
					},
				}, nil
			}
			cancel()
			return azqueue.DequeueMessagesResponse{}, nil
		},
		deleteFn: func(_ context.Context, _ string, _ string, _ *azqueue.DeleteMessageOptions) (azqueue.DeleteMessageResponse, error) {
			atomic.AddInt64(&deleteCallCount, 1)
			return azqueue.DeleteMessageResponse{}, nil
		},
	}

	p := &azureProvider{
		queueClient: mq,
		api:         newTestAPI(),
		cfg:         AzureQueueProviderConfig{QueueName: "test-q"},
		handler: func(data []byte) error {
			return fmt.Errorf("handler failure")
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.pollQueue(ctx)
	}()

	select {
	case <-done:
		assert.Equal(t, int64(0), atomic.LoadInt64(&deleteCallCount))
	case <-time.After(10 * time.Second):
		t.Fatal("pollQueue did not exit in time")
	}
}

func TestAzurePollQueue_HandlerSuccess_DeletesMessage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var deletedID string
	callNum := int64(0)
	payload := base64.StdEncoding.EncodeToString([]byte("good payload"))

	mq := &mockAzureQueue{
		dequeueFn: func(_ context.Context, _ *azqueue.DequeueMessagesOptions) (azqueue.DequeueMessagesResponse, error) {
			n := atomic.AddInt64(&callNum, 1)
			if n == 1 {
				return azqueue.DequeueMessagesResponse{
					Messages: []*azqueue.DequeuedMessage{
						{
							MessageText: &payload,
							MessageID:   new("msg-3"),
							PopReceipt:  new("receipt-3"),
						},
					},
				}, nil
			}
			cancel()
			return azqueue.DequeueMessagesResponse{}, nil
		},
		deleteFn: func(_ context.Context, messageID string, _ string, _ *azqueue.DeleteMessageOptions) (azqueue.DeleteMessageResponse, error) {
			deletedID = messageID
			return azqueue.DeleteMessageResponse{}, nil
		},
	}

	p := &azureProvider{
		queueClient: mq,
		api:         newTestAPI(),
		cfg:         AzureQueueProviderConfig{QueueName: "test-q"},
		handler: func(data []byte) error {
			return nil
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.pollQueue(ctx)
	}()

	select {
	case <-done:
		assert.Equal(t, "msg-3", deletedID)
	case <-time.After(10 * time.Second):
		t.Fatal("pollQueue did not exit in time")
	}
}

func TestAzurePollQueue_NilFields_Skipped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var deleteCallCount int64
	callNum := int64(0)

	mq := &mockAzureQueue{
		dequeueFn: func(_ context.Context, _ *azqueue.DequeueMessagesOptions) (azqueue.DequeueMessagesResponse, error) {
			n := atomic.AddInt64(&callNum, 1)
			if n == 1 {
				return azqueue.DequeueMessagesResponse{
					Messages: []*azqueue.DequeuedMessage{
						{
							MessageText: nil,
							MessageID:   new("m1"),
							PopReceipt:  new("r1"),
						},
						{
							MessageText: new("text"),
							MessageID:   nil,
							PopReceipt:  new("r2"),
						},
						{
							MessageText: new("text"),
							MessageID:   new("m3"),
							PopReceipt:  nil,
						},
					},
				}, nil
			}
			cancel()
			return azqueue.DequeueMessagesResponse{}, nil
		},
		deleteFn: func(_ context.Context, _ string, _ string, _ *azqueue.DeleteMessageOptions) (azqueue.DeleteMessageResponse, error) {
			atomic.AddInt64(&deleteCallCount, 1)
			return azqueue.DeleteMessageResponse{}, nil
		},
	}

	handlerCalled := int64(0)
	p := &azureProvider{
		queueClient: mq,
		api:         newTestAPI(),
		cfg:         AzureQueueProviderConfig{QueueName: "test-q"},
		handler: func(data []byte) error {
			atomic.AddInt64(&handlerCalled, 1)
			return nil
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.pollQueue(ctx)
	}()

	select {
	case <-done:
		assert.Equal(t, int64(0), atomic.LoadInt64(&handlerCalled), "handler should not be called for nil-field messages")
		assert.Equal(t, int64(0), atomic.LoadInt64(&deleteCallCount), "delete should not be called for nil-field messages")
	case <-time.After(10 * time.Second):
		t.Fatal("pollQueue did not exit in time")
	}
}

func TestAzureSubscribe_InitializesFields(t *testing.T) {
	mq := &mockAzureQueue{
		dequeueFn: func(ctx context.Context, _ *azqueue.DequeueMessagesOptions) (azqueue.DequeueMessagesResponse, error) {
			<-ctx.Done()
			return azqueue.DequeueMessagesResponse{}, ctx.Err()
		},
	}

	p := &azureProvider{
		queueClient: mq,
		api:         newTestAPI(),
		cfg:         AzureQueueProviderConfig{QueueName: "test-q"},
	}

	handler := func(data []byte) error { return nil }
	err := p.Subscribe(context.Background(), handler)
	require.NoError(t, err)

	assert.NotNil(t, p.cancel, "cancel should be set after Subscribe")
	assert.NotNil(t, p.handler, "handler should be set after Subscribe")
	assert.NotNil(t, p.pollDone, "pollDone should be set after Subscribe")

	require.NoError(t, p.Close())
}

func TestAzureClose_WaitsPollDone(t *testing.T) {
	mq := &mockAzureQueue{
		dequeueFn: func(ctx context.Context, _ *azqueue.DequeueMessagesOptions) (azqueue.DequeueMessagesResponse, error) {
			<-ctx.Done()
			return azqueue.DequeueMessagesResponse{}, ctx.Err()
		},
	}

	p := &azureProvider{
		queueClient: mq,
		api:         newTestAPI(),
		cfg:         AzureQueueProviderConfig{QueueName: "test-q"},
	}

	handler := func(data []byte) error { return nil }
	err := p.Subscribe(context.Background(), handler)
	require.NoError(t, err)

	closeDone := make(chan struct{})
	go func() {
		defer close(closeDone)
		_ = p.Close()
	}()

	select {
	case <-closeDone:
		// Close returned, meaning it waited for pollDone.
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return in time, may not be waiting for pollDone")
	}
}

func TestAzureClose_CancelsContext(t *testing.T) {
	var cancelCalled int64

	ctx, originalCancel := context.WithCancel(context.Background())

	p := &azureProvider{
		api: newTestAPI(),
		cancel: func() {
			atomic.AddInt64(&cancelCalled, 1)
			originalCancel()
		},
		pollDone: make(chan struct{}),
	}

	// Simulate pollDone being already closed (goroutine finished).
	close(p.pollDone)

	err := p.Close()
	require.NoError(t, err)
	assert.Equal(t, int64(1), atomic.LoadInt64(&cancelCalled), "cancel function should be called exactly once")

	select {
	case <-ctx.Done():
		// Context was cancelled as expected.
	default:
		t.Fatal("underlying context was not cancelled")
	}
}
