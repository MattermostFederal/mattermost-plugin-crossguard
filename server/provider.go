package main

import "context"

// QueueProvider abstracts the message transport and file transfer layer.
// Both NATS and Azure Queue Storage implement this interface.
type QueueProvider interface {
	// Publish sends a message. Includes internal retries appropriate to transport.
	// A returned error is a final failure.
	Publish(ctx context.Context, data []byte) error

	// Subscribe starts delivering messages to the handler.
	// NATS: push-based subscription. Azure: polling goroutine.
	// Handler returning nil = message processed (Azure deletes it).
	// Handler returning error = message not processed (Azure lets visibility timeout expire).
	Subscribe(ctx context.Context, handler func(data []byte) error) error

	// UploadFile uploads a file with metadata.
	UploadFile(ctx context.Context, key string, data []byte, headers map[string]string) error

	// WatchFiles watches for new files and calls handler.
	// Handler returning nil = file processed (provider may clean up).
	// Handler returning error = file not processed (retry later).
	WatchFiles(ctx context.Context, handler func(key string, data []byte, headers map[string]string) error) error

	// MaxMessageSize returns the provider's message size limit in bytes.
	// Returns 0 for no limit. Caller checks before Publish.
	MaxMessageSize() int

	// Close gracefully shuts down. In-flight operations complete or are abandoned.
	Close() error
}
