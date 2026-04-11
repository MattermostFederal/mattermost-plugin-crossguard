package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAzureBlobProvider_InterfaceConformance(t *testing.T) {
	var _ QueueProvider = (*azureBlobProvider)(nil)
}

func TestBlobLockKey_HashesStably(t *testing.T) {
	k1 := blobLockKey("messages/high/node-1-42.jsonl")
	k2 := blobLockKey("messages/high/node-1-42.jsonl")
	assert.Equal(t, k1, k2)
	assert.True(t, strings.HasPrefix(k1, blobLockKeyPrefix))

	k3 := blobLockKey("messages/high/other.jsonl")
	assert.NotEqual(t, k1, k3)
}

func TestBlobLockKey_UnderKVLengthLimit(t *testing.T) {
	long := strings.Repeat("x", 2000)
	k := blobLockKey("messages/conn/" + long + ".jsonl")
	// pluginapi KV keys must be <= 150 chars; our hashed key should be far below.
	assert.LessOrEqual(t, len(k), 64)
}

func TestAzureBlobProvider_MaxMessageSize(t *testing.T) {
	a := &azureBlobProvider{}
	assert.Equal(t, 0, a.MaxMessageSize())
}

func TestPendingFileRef_JSONRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "batch.files.json")

	refs := []pendingFileRef{
		{PostID: "p1", FileID: "f1", ConnName: "high", Filename: "report.pdf"},
		{PostID: "p2", FileID: "f2", ConnName: "low", Filename: "spec.txt"},
	}

	data, err := json.Marshal(refs)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))

	raw, err := os.ReadFile(path) //nolint:gosec // path built from t.TempDir

	require.NoError(t, err)

	var got []pendingFileRef
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, refs, got)
}
