package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
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
