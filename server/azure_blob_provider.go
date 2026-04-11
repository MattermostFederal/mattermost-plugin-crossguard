package main

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // used for blob lock key hashing, not cryptographic security
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi"
)

const (
	// walDirRoot is the root directory holding WAL files under os.TempDir.
	walDirRoot = "crossguard-wal"

	// walMaxFiles is the backpressure threshold for unflushed WAL files.
	walMaxFiles = 100

	// walFileExt is the extension for WAL message batch files.
	walFileExt = ".jsonl"

	// walFilesExt is the extension for the companion deferred file manifest.
	walFilesExt = ".files.json"

	// blobMessagePrefix is the prefix for message batch blobs.
	blobMessagePrefix = "messages/"

	// blobFilesPrefix is the prefix for file attachment blobs.
	blobFilesPrefix = "files/"

	// blobLockKeyPrefix is the KV store prefix for per-blob locks.
	blobLockKeyPrefix = "blob-lock-"

	// blobLockMaxAge is the maximum lock age before it is considered stale.
	blobLockMaxAge = 5 * time.Minute
)

// kvClient is the minimal KV interface needed by the azure-blob provider for
// distributed blob locking. It wraps the pluginapi KV client so it can be
// mocked in tests.
type kvClient interface {
	Get(key string, o any) error
	Set(key string, value any, options ...pluginapi.KVSetOption) (bool, error)
	Delete(key string) error
}

// blobListing is a minimal view of a blob returned by list operations.
type blobListing struct {
	Name     string
	Metadata map[string]*string
}

// azureBlobOps abstracts the Azure Blob container operations used by
// azureBlobProvider so tests can inject a fake implementation without
// hitting the Azure SDK.
type azureBlobOps interface {
	CreateContainer(ctx context.Context) error
	UploadBlob(ctx context.Context, name string, data []byte, metadata map[string]*string) error
	DownloadBlob(ctx context.Context, name string) ([]byte, error)
	DeleteBlob(ctx context.Context, name string) error
	ListBlobs(ctx context.Context, prefix string, includeMetadata bool) ([]blobListing, error)
}

// containerClientAdapter wraps *container.Client to implement azureBlobOps.
type containerClientAdapter struct {
	client *container.Client
}

func (c *containerClientAdapter) CreateContainer(ctx context.Context) error {
	_, err := c.client.Create(ctx, nil)
	return err
}

func (c *containerClientAdapter) UploadBlob(ctx context.Context, name string, data []byte, metadata map[string]*string) error {
	bc := c.client.NewBlockBlobClient(name)
	var opts *blockblob.UploadOptions
	if metadata != nil {
		opts = &blockblob.UploadOptions{Metadata: metadata}
	}
	_, err := bc.Upload(ctx, newNopCloser(data), opts)
	return err
}

func (c *containerClientAdapter) DownloadBlob(ctx context.Context, name string) ([]byte, error) {
	bc := c.client.NewBlockBlobClient(name)
	resp, err := bc.DownloadStream(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}

func (c *containerClientAdapter) DeleteBlob(ctx context.Context, name string) error {
	bc := c.client.NewBlockBlobClient(name)
	_, err := bc.Delete(ctx, nil)
	return err
}

func (c *containerClientAdapter) ListBlobs(ctx context.Context, prefix string, includeMetadata bool) ([]blobListing, error) {
	opts := &container.ListBlobsFlatOptions{}
	if prefix != "" {
		opts.Prefix = &prefix
	}
	if includeMetadata {
		opts.Include = container.ListBlobsInclude{Metadata: true}
	}
	pager := c.client.NewListBlobsFlatPager(opts)
	var result []blobListing
	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return result, err
		}
		for _, b := range resp.Segment.BlobItems {
			if b.Name == nil {
				continue
			}
			result = append(result, blobListing{Name: *b.Name, Metadata: b.Metadata})
		}
	}
	return result, nil
}

// blobLock is the JSON value stored under a blob lock key.
type blobLock struct {
	Node     string `json:"node"`
	Acquired int64  `json:"acquired"`
}

// pendingFileRef captures file metadata to upload after WAL flush confirms.
type pendingFileRef struct {
	PostID   string `json:"post_id"`
	FileID   string `json:"file_id"`
	ConnName string `json:"conn_name"`
	Filename string `json:"filename"`
}

// getFileFunc loads a file's bytes by file ID. It is injected to avoid a
// direct dependency on plugin.API and to simplify testing.
type getFileFunc func(fileID string) ([]byte, error)

// azureBlobProvider implements QueueProvider using Azure Blob Storage for
// batched message relay via .jsonl files.
type azureBlobProvider struct {
	containerClient azureBlobOps
	api             plugin.API
	kv              kvClient
	cfg             AzureBlobProviderConfig
	nodeID          string
	connName        string
	flushInterval   time.Duration
	getFile         getFileFunc
	isOutbound      bool

	// Lifetime context (provided by the plugin) used for outbound flush loop.
	outCtx    context.Context
	outCancel context.CancelFunc

	// WAL (outbound only)
	walMu   sync.Mutex
	walFile *os.File
	walPath string
	walSeq  atomic.Int64
	walDir  string

	// Pending file refs (outbound only)
	pendingFilesMu sync.Mutex
	pendingFiles   []pendingFileRef

	// Flush control (outbound only)
	flushTicker *time.Ticker
	flushStop   chan struct{}
	flushDone   chan struct{}

	// Inbound polling
	cancel   context.CancelFunc
	pollDone chan struct{}
	handler  func(data []byte) error

	// closeOnce guards Close from concurrent invocation.
	closeOnce sync.Once
}

// newAzureBlobProvider constructs an azure-blob provider. If isOutbound is
// true, the provider immediately runs WAL crash recovery and starts the flush
// loop using the supplied ctx (which should be the plugin lifetime context).
func newAzureBlobProvider(ctx context.Context, cfg AzureBlobProviderConfig, api plugin.API, kv kvClient, nodeID, connName string, getFile getFileFunc, isOutbound bool) (*azureBlobProvider, error) {
	cred, err := container.NewSharedKeyCredential(cfg.AccountName, cfg.AccountKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure Blob shared key credential: %w", err)
	}
	containerClient, err := container.NewClientWithSharedKeyCredential(buildBlobContainerURL(cfg.ServiceURL, cfg.BlobContainerName), cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure Blob container client: %w", err)
	}

	ops := &containerClientAdapter{client: containerClient}

	// Ensure container exists (idempotent). Treat any non-"already exists"
	// error as a hard failure so misconfiguration surfaces at startup.
	createCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if createErr := ops.CreateContainer(createCtx); createErr != nil {
		if !strings.Contains(createErr.Error(), azureErrContainerAlreadyExists) {
			return nil, fmt.Errorf("failed to create/access Azure Blob container %q: %w", cfg.BlobContainerName, createErr)
		}
	}

	return newAzureBlobProviderFromOps(ctx, cfg, api, kv, nodeID, connName, getFile, isOutbound, ops)
}

// newAzureBlobProviderFromOps constructs a provider from a pre-built
// azureBlobOps. Used by both the public constructor (after wrapping the SDK
// client in an adapter) and tests, which can pass a fake ops implementation.
func newAzureBlobProviderFromOps(ctx context.Context, cfg AzureBlobProviderConfig, api plugin.API, kv kvClient, nodeID, connName string, getFile getFileFunc, isOutbound bool, ops azureBlobOps) (*azureBlobProvider, error) {
	flushSec := cfg.FlushIntervalSeconds
	if flushSec <= 0 {
		flushSec = defaultAzureBlobFlushIntervalSec
	}

	// Scope the WAL directory by (nodeID, connName) so multiple azure-blob
	// connections on the same node do not collide on filenames or recovery.
	walDir := filepath.Join(os.TempDir(), walDirRoot, nodeID, connName)
	if err := os.MkdirAll(walDir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create WAL directory %q: %w", walDir, err)
	}

	api.LogWarn("Azure Blob provider: WAL directory is in temp storage and may not survive container restarts",
		"wal_dir", walDir)

	a := &azureBlobProvider{
		containerClient: ops,
		api:             api,
		kv:              kv,
		cfg:             cfg,
		nodeID:          nodeID,
		connName:        connName,
		flushInterval:   time.Duration(flushSec) * time.Second,
		getFile:         getFile,
		walDir:          walDir,
		isOutbound:      isOutbound,
	}

	if isOutbound {
		a.outCtx, a.outCancel = context.WithCancel(ctx)
		a.recoverWALOnStartup(a.outCtx)
		a.startFlushLoop(a.outCtx)
	}

	return a, nil
}

// Publish appends the message to the current WAL file.
func (a *azureBlobProvider) Publish(_ context.Context, data []byte) error {
	a.walMu.Lock()
	defer a.walMu.Unlock()

	// Backpressure: if too many unflushed WAL files accumulated, reject.
	if n := a.countWALFiles(); n >= walMaxFiles {
		return fmt.Errorf("WAL backpressure: %d unflushed files (limit %d)", n, walMaxFiles)
	}

	if a.walFile == nil {
		if err := a.openWALFileLocked(); err != nil {
			return err
		}
	}

	// Two writes to avoid `append(data, '\n')` mutating the caller's buffer
	// when cap(data) > len(data).
	if _, err := a.walFile.Write(data); err != nil {
		return fmt.Errorf("failed to write to WAL: %w", err)
	}
	if _, err := a.walFile.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("failed to write to WAL: %w", err)
	}
	return nil
}

// openWALFileLocked opens a new WAL file. Must be called with walMu held.
func (a *azureBlobProvider) openWALFileLocked() error {
	seq := a.walSeq.Add(1)
	name := fmt.Sprintf("%s-%d-%d%s", a.nodeID, time.Now().UnixMilli(), seq, walFileExt)
	path := filepath.Join(a.walDir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // path built from walDir + nodeID/seq/timestamp
	if err != nil {
		return fmt.Errorf("failed to open WAL file %q: %w", path, err)
	}
	a.walFile = f
	a.walPath = path
	return nil
}

// countWALFiles returns the number of .jsonl files in walDir.
func (a *azureBlobProvider) countWALFiles() int {
	entries, err := os.ReadDir(a.walDir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), walFileExt) {
			n++
		}
	}
	return n
}

// QueueFileRef records a pending file upload to be performed after the next
// WAL flush. The file ref is also persisted to a companion .files.json file
// so it can be recovered on crash.
//
// Lock order: walMu before pendingFilesMu. This matches flush() and must not
// be reversed (would create an AB/BA deadlock with flush).
func (a *azureBlobProvider) QueueFileRef(postID, fileID, connName, filename string) {
	a.walMu.Lock()
	defer a.walMu.Unlock()
	a.pendingFilesMu.Lock()
	defer a.pendingFilesMu.Unlock()

	a.pendingFiles = append(a.pendingFiles, pendingFileRef{
		PostID:   postID,
		FileID:   fileID,
		ConnName: connName,
		Filename: filename,
	})

	// Best effort: write the companion file using the current WAL path base
	// so crash recovery can match the two. If walPath is empty (no WAL yet),
	// skip and rely on the next Publish to open a WAL file; the next flush
	// will snapshot pending files at that point.
	if a.walPath == "" {
		return
	}
	companion := a.walPath[:len(a.walPath)-len(walFileExt)] + walFilesExt
	data, err := json.Marshal(a.pendingFiles)
	if err != nil {
		a.api.LogWarn("Azure Blob: failed to marshal pending files", "error", err.Error())
		return
	}
	if err := os.WriteFile(companion, data, 0o600); err != nil {
		a.api.LogWarn("Azure Blob: failed to write companion files.json",
			"path", companion, "error", err.Error())
	}
}

// startFlushLoop kicks off the flush ticker goroutine.
func (a *azureBlobProvider) startFlushLoop(ctx context.Context) {
	a.flushTicker = time.NewTicker(a.flushInterval)
	a.flushStop = make(chan struct{})
	a.flushDone = make(chan struct{})

	go func() {
		defer close(a.flushDone)
		defer a.flushTicker.Stop()
		for {
			select {
			case <-a.flushStop:
				// Final flush on graceful shutdown, bounded so a hung Azure
				// upload cannot block plugin deactivation.
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				a.flush(shutdownCtx)
				cancel()
				return
			case <-ctx.Done():
				return
			case <-a.flushTicker.C:
				a.flush(ctx)
			}
		}
	}()
}

// flush rotates the WAL, uploads the closed file, and then uploads deferred
// file refs. On upload failure, the WAL file is left on disk for recovery.
func (a *azureBlobProvider) flush(ctx context.Context) {
	// Step 1-4: Rotate WAL under both locks.
	a.walMu.Lock()
	a.pendingFilesMu.Lock()

	if a.walFile == nil && len(a.pendingFiles) == 0 {
		a.pendingFilesMu.Unlock()
		a.walMu.Unlock()
		return
	}

	var oldWALPath string
	if a.walFile != nil {
		if err := a.walFile.Close(); err != nil {
			a.api.LogError("Azure Blob: WAL close failed during rotation, skipping upload",
				"path", a.walPath, "error", err.Error())
			a.walFile = nil
			a.walPath = ""
			a.pendingFilesMu.Unlock()
			a.walMu.Unlock()
			return
		}
		oldWALPath = a.walPath
		a.walFile = nil
		a.walPath = ""
	}

	oldPending := a.pendingFiles
	a.pendingFiles = nil

	a.pendingFilesMu.Unlock()
	a.walMu.Unlock()

	// Step 5: Upload the old WAL file.
	if oldWALPath != "" {
		if err := a.uploadWALFile(ctx, oldWALPath); err != nil {
			a.api.LogError("Azure Blob: WAL upload failed, leaving for recovery",
				"path", oldWALPath, "error", err.Error())
			return
		}
		if err := os.Remove(oldWALPath); err != nil && !os.IsNotExist(err) {
			a.api.LogWarn("Azure Blob: failed to delete WAL after upload",
				"path", oldWALPath, "error", err.Error())
		}
	}

	// Step 6: Upload deferred file refs.
	if len(oldPending) > 0 {
		a.flushPendingFilesList(ctx, oldPending)
	}

	// Remove companion files.json for the flushed batch.
	if oldWALPath != "" {
		companion := oldWALPath[:len(oldWALPath)-len(walFileExt)] + walFilesExt
		if err := os.Remove(companion); err != nil && !os.IsNotExist(err) {
			a.api.LogWarn("Azure Blob: failed to delete companion files.json",
				"path", companion, "error", err.Error())
		}
	}
}

// uploadWALFile uploads a WAL file as a message batch blob.
func (a *azureBlobProvider) uploadWALFile(ctx context.Context, path string) error {
	data, err := os.ReadFile(path) //nolint:gosec // path is constructed from trusted nodeID/seq/timestamp
	if err != nil {
		return fmt.Errorf("read WAL: %w", err)
	}
	if len(data) == 0 {
		return nil // nothing to upload
	}

	base := filepath.Base(path)
	blobName := blobMessagePrefix + a.connName + "/" + base

	if err := a.containerClient.UploadBlob(ctx, blobName, data, nil); err != nil {
		return fmt.Errorf("upload blob %q: %w", blobName, err)
	}
	return nil
}

// flushPendingFilesList uploads each pending file ref. Failures are re-enqueued
// so they are retried on the next flush cycle.
func (a *azureBlobProvider) flushPendingFilesList(ctx context.Context, refs []pendingFileRef) {
	var failed []pendingFileRef
	for _, ref := range refs {
		data, err := a.getFile(ref.FileID)
		if err != nil {
			a.api.LogError("Azure Blob: deferred file fetch failed",
				"file_id", ref.FileID, "post_id", ref.PostID, "error", err.Error())
			continue
		}
		key := ref.PostID + "/" + ref.FileID
		headers := map[string]string{
			headerPostID:   ref.PostID,
			headerConnName: ref.ConnName,
			headerFilename: ref.Filename,
		}
		if err := a.UploadFile(ctx, key, data, headers); err != nil {
			a.api.LogError("Azure Blob: deferred file upload failed",
				"file_id", ref.FileID, "post_id", ref.PostID, "error", err.Error())
			failed = append(failed, ref)
		}
	}
	if len(failed) > 0 {
		a.pendingFilesMu.Lock()
		a.pendingFiles = append(failed, a.pendingFiles...)
		a.pendingFilesMu.Unlock()
	}
}

// Subscribe starts the inbound poll loop. The outbound flush loop is started
// separately by the constructor when isOutbound is true, so inbound-only
// providers do not run an idle flush loop.
func (a *azureBlobProvider) Subscribe(ctx context.Context, handler func(data []byte) error) error {
	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	a.handler = handler
	a.pollDone = make(chan struct{})

	go func() {
		defer close(a.pollDone)
		a.pollBlobs(ctx)
	}()

	return nil
}

// pollBlobs periodically lists and processes message batch blobs.
func (a *azureBlobProvider) pollBlobs(ctx context.Context) {
	prefix := blobMessagePrefix + a.connName + "/"
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(azureBlobBatchPollInterval):
		}

		blobs, err := a.containerClient.ListBlobs(ctx, prefix, false)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			a.api.LogError("Azure Blob: list failed", "container", a.cfg.BlobContainerName, "error", err.Error())
			continue
		}
		for _, b := range blobs {
			a.processBlob(ctx, b.Name)
		}
	}
}

// processBlob acquires a lock, downloads, processes line-by-line, and
// deletes the blob on success.
func (a *azureBlobProvider) processBlob(ctx context.Context, blobName string) {
	if !a.tryAcquireBlobLock(blobName) {
		return
	}

	success := false
	defer func() {
		// Release lock on failure so another node can retry.
		if !success {
			a.releaseBlobLock(blobName)
		}
	}()

	data, err := a.containerClient.DownloadBlob(ctx, blobName)
	if err != nil {
		a.api.LogWarn("Azure Blob: download failed", "blob", blobName, "error", err.Error())
		return
	}

	for line := range bytes.SplitSeq(data, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		if err := a.handler(line); err != nil {
			a.api.LogWarn("Azure Blob: handler error, will retry blob",
				"blob", blobName, "error", err.Error())
			return
		}
	}

	if err := a.containerClient.DeleteBlob(ctx, blobName); err != nil {
		a.api.LogWarn("Azure Blob: delete failed", "blob", blobName, "error", err.Error())
		return
	}

	success = true
	a.releaseBlobLock(blobName)
}

// tryAcquireBlobLock acquires the KV lock for a blob name. Returns true if
// this node owns the lock (either freshly acquired or stale-reclaimed).
func (a *azureBlobProvider) tryAcquireBlobLock(blobName string) bool {
	key := blobLockKey(blobName)

	var raw []byte
	if err := a.kv.Get(key, &raw); err != nil {
		a.api.LogWarn("Azure Blob: lock get failed", "blob", blobName, "error", err.Error())
		return false
	}

	newLock := blobLock{Node: a.nodeID, Acquired: time.Now().UnixMilli()}

	if len(raw) == 0 {
		ok, err := a.kv.Set(key, newLock, pluginapi.SetAtomic(nil))
		if err != nil {
			a.api.LogWarn("Azure Blob: lock set failed", "blob", blobName, "error", err.Error())
			return false
		}
		return ok
	}

	var current blobLock
	if err := json.Unmarshal(raw, &current); err != nil {
		// Corrupt lock value; try to reclaim.
		ok, _ := a.kv.Set(key, newLock, pluginapi.SetAtomic(raw))
		return ok
	}

	age := time.Since(time.UnixMilli(current.Acquired))
	if age < blobLockMaxAge {
		return false
	}

	ok, err := a.kv.Set(key, newLock, pluginapi.SetAtomic(raw))
	if err != nil {
		a.api.LogWarn("Azure Blob: stale lock reclaim failed", "blob", blobName, "error", err.Error())
		return false
	}
	return ok
}

// releaseBlobLock deletes the KV lock for a blob name.
func (a *azureBlobProvider) releaseBlobLock(blobName string) {
	key := blobLockKey(blobName)
	if err := a.kv.Delete(key); err != nil {
		a.api.LogWarn("Azure Blob: lock release failed", "blob", blobName, "error", err.Error())
	}
}

// blobLockKey returns a KV key for a blob name, hashing to stay under the KV
// key length limit.
func blobLockKey(blobName string) string {
	h := sha1.Sum([]byte(blobName)) //nolint:gosec // key derivation, not crypto
	return blobLockKeyPrefix + hex.EncodeToString(h[:])
}

// recoverWALOnStartup scans the parent WAL root for leftover files from
// previous runs and re-uploads them. This handles the case where nodeID
// changes across restarts (orphaning the previous node's WAL directory).
// Only directories for this provider's connName are processed, so multiple
// azure-blob connections on the same node do not interfere.
func (a *azureBlobProvider) recoverWALOnStartup(ctx context.Context) {
	root := filepath.Join(os.TempDir(), walDirRoot)
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			a.api.LogWarn("Azure Blob: WAL recovery: failed to scan root",
				"root", root, "error", err.Error())
		}
		return
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		nodeDir := filepath.Join(root, e.Name())
		connDir := filepath.Join(nodeDir, a.connName)
		if info, statErr := os.Stat(connDir); statErr != nil || !info.IsDir() {
			continue
		}
		isCurrent := e.Name() == a.nodeID
		a.recoverDirectory(ctx, connDir, isCurrent)
		if !isCurrent {
			// Remove the old nodeID parent if now empty. Ignore non-empty errors.
			_ = os.Remove(nodeDir)
		}
	}
}

// recoverDirectory uploads WAL files and processes companion .files.json in a
// WAL directory. If isCurrent is false, the directory is removed after
// recovery (it belongs to a previous nodeID).
func (a *azureBlobProvider) recoverDirectory(ctx context.Context, dir string, isCurrent bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		a.api.LogWarn("Azure Blob: WAL recovery: failed to scan directory",
			"dir", dir, "error", err.Error())
		return
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		path := filepath.Join(dir, name)

		switch {
		case strings.HasSuffix(name, walFileExt):
			if err := a.uploadWALFile(ctx, path); err != nil {
				a.api.LogError("Azure Blob: WAL recovery upload failed",
					"path", path, "error", err.Error())
				continue
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				a.api.LogWarn("Azure Blob: WAL recovery delete failed",
					"path", path, "error", err.Error())
			}
		case strings.HasSuffix(name, walFilesExt):
			a.recoverCompanionFiles(ctx, path)
		}
	}

	if !isCurrent {
		// Remove the old nodeID directory if empty.
		_ = os.Remove(dir)
	}
}

// recoverCompanionFiles reads a companion .files.json and uploads the listed files.
func (a *azureBlobProvider) recoverCompanionFiles(ctx context.Context, path string) {
	raw, err := os.ReadFile(path) //nolint:gosec // path from walDir scan
	if err != nil {
		a.api.LogWarn("Azure Blob: WAL recovery: failed to read companion file",
			"path", path, "error", err.Error())
		return
	}
	var refs []pendingFileRef
	if err := json.Unmarshal(raw, &refs); err != nil {
		a.api.LogWarn("Azure Blob: WAL recovery: malformed companion file",
			"path", path, "error", err.Error())
		_ = os.Remove(path)
		return
	}
	if len(refs) == 0 {
		_ = os.Remove(path)
		return
	}
	a.flushPendingFilesList(ctx, refs)
	_ = os.Remove(path)
}

// UploadFile uploads a file blob with metadata headers.
func (a *azureBlobProvider) UploadFile(ctx context.Context, key string, data []byte, headers map[string]string) error {
	blobName := blobFilesPrefix + key

	meta := azureBlobMetadata{Headers: headers}
	metaJSON, _ := json.Marshal(meta)
	encoded := base64.StdEncoding.EncodeToString(metaJSON)
	blobMeta := map[string]*string{
		blobMetadataHeadersKey: &encoded,
	}

	if err := a.containerClient.UploadBlob(ctx, blobName, data, blobMeta); err != nil {
		return fmt.Errorf("failed to upload blob %q: %w", blobName, err)
	}
	return nil
}

// WatchFiles watches the files/ prefix for new file blobs.
func (a *azureBlobProvider) WatchFiles(ctx context.Context, handler func(key string, data []byte, headers map[string]string) error) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(azureBlobBatchPollInterval):
		}

		blobs, err := a.containerClient.ListBlobs(ctx, blobFilesPrefix, true)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			a.api.LogError("Azure Blob: file list failed", "container", a.cfg.BlobContainerName, "error", err.Error())
			continue
		}

		for _, blob := range blobs {
			// HA: use blob lock to avoid duplicate file processing.
			if !a.tryAcquireBlobLock(blob.Name) {
				continue
			}

			success := false
			func() {
				defer func() {
					if !success {
						a.releaseBlobLock(blob.Name)
					}
				}()

				headers := extractBlobHeaders(blob.Metadata)
				data, err := a.containerClient.DownloadBlob(ctx, blob.Name)
				if err != nil {
					a.api.LogWarn("Azure Blob: file download failed", "blob", blob.Name, "error", err.Error())
					return
				}
				// Strip the files/ prefix when passing key to handler to match azureProvider behaviour.
				key := strings.TrimPrefix(blob.Name, blobFilesPrefix)
				if err := handler(key, data, headers); err != nil {
					a.api.LogWarn("Azure Blob: file handler error", "blob", blob.Name, "error", err.Error())
					return
				}
				if err := a.containerClient.DeleteBlob(ctx, blob.Name); err != nil {
					a.api.LogWarn("Azure Blob: file delete failed", "blob", blob.Name, "error", err.Error())
					return
				}
				success = true
				a.releaseBlobLock(blob.Name)
			}()
		}
	}
}

// MaxMessageSize returns 0 because the azure-blob provider batches messages
// into files and does not impose a per-message size limit.
func (a *azureBlobProvider) MaxMessageSize() int {
	return 0
}

// Close flushes pending data and stops background goroutines. Safe to call
// concurrently; subsequent calls are no-ops.
func (a *azureBlobProvider) Close() error {
	a.closeOnce.Do(func() {
		// Signal the outbound flush loop to stop and wait for it.
		if a.flushStop != nil {
			close(a.flushStop)
		}
		if a.flushDone != nil {
			<-a.flushDone
		}

		// Cancel the outbound lifetime context so anything derived from it
		// (e.g. in-flight Azure SDK calls) unwinds.
		if a.outCancel != nil {
			a.outCancel()
		}

		// Stop the inbound poll loop.
		if a.cancel != nil {
			a.cancel()
		}
		if a.pollDone != nil {
			<-a.pollDone
		}
	})
	return nil
}

// testAzureBlobConnection probes an azure-blob connection by creating a test
// blob and deleting it.
func testAzureBlobConnection(cfg AzureBlobProviderConfig) error {
	cred, err := container.NewSharedKeyCredential(cfg.AccountName, cfg.AccountKey)
	if err != nil {
		return fmt.Errorf("failed to create shared key credential: %w", err)
	}
	containerClient, err := container.NewClientWithSharedKeyCredential(buildBlobContainerURL(cfg.ServiceURL, cfg.BlobContainerName), cred, nil)
	if err != nil {
		return fmt.Errorf("failed to create container client: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return testAzureBlobConnectionOps(ctx, &containerClientAdapter{client: containerClient})
}

// testAzureBlobConnectionOps contains the provider-agnostic core of the
// connection test. Extracted so tests can inject a fake azureBlobOps.
func testAzureBlobConnectionOps(ctx context.Context, ops azureBlobOps) error {
	if createErr := ops.CreateContainer(ctx); createErr != nil {
		if !strings.Contains(createErr.Error(), azureErrContainerAlreadyExists) {
			return fmt.Errorf("failed to create container: %w", createErr)
		}
	}

	key := fmt.Sprintf("crossguard-test-%d", time.Now().UnixMilli())
	if err := ops.UploadBlob(ctx, key, []byte("ok"), nil); err != nil {
		return fmt.Errorf("failed to upload test blob: %w", err)
	}
	if err := ops.DeleteBlob(ctx, key); err != nil {
		return fmt.Errorf("failed to delete test blob: %w", err)
	}
	return nil
}

// ensure interface compliance at compile time
var _ QueueProvider = (*azureBlobProvider)(nil)
