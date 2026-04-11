package main

import (
	"context"
	"sync"
	"time"
)

const (
	retryQueueMaxSize       = 1000
	retryQueueMaxRetries    = 3
	retryQueueDefaultMaxAge = 2 * time.Minute
	retryQueueTickRate      = 5 * time.Second
	retryQueueRetryDelay    = 20 * time.Second
)

type retryEntry struct {
	connName   string
	rawData    []byte
	remoteID   string
	msgType    string
	enqueuedAt time.Time
	lastRetry  time.Time
	retries    int
}

type retryQueue struct {
	mu      sync.Mutex
	entries []*retryEntry
	maxAge  time.Duration
	done    chan struct{}
}

func newRetryQueue(maxAge time.Duration) *retryQueue {
	if maxAge <= 0 {
		maxAge = retryQueueDefaultMaxAge
	}
	return &retryQueue{
		maxAge: maxAge,
	}
}

// Enqueue adds a message to the retry queue. Returns false if the queue is full.
func (q *retryQueue) Enqueue(connName string, data []byte, remoteID, msgType string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.entries) >= retryQueueMaxSize {
		return false
	}

	cp := make([]byte, len(data))
	copy(cp, data)

	q.entries = append(q.entries, &retryEntry{
		connName:   connName,
		rawData:    cp,
		remoteID:   remoteID,
		msgType:    msgType,
		enqueuedAt: time.Now(),
		lastRetry:  time.Now(),
	})
	return true
}

// Len returns the current queue size.
func (q *retryQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.entries)
}

// SetMaxAge updates the max age for retry entries.
func (q *retryQueue) SetMaxAge(maxAge time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.maxAge = maxAge
}

// Drain returns entries that are ready for retry (past the retry delay) and
// removes expired/exhausted entries. Entries returned for retry remain in the
// queue; the caller must call Remove after successful processing.
func (q *retryQueue) Drain(now time.Time) (ready []*retryEntry, dropped []*retryEntry) {
	q.mu.Lock()
	defer q.mu.Unlock()

	var kept []*retryEntry
	for _, e := range q.entries {
		age := now.Sub(e.enqueuedAt)
		if age >= q.maxAge {
			dropped = append(dropped, e)
			continue
		}
		if e.retries >= retryQueueMaxRetries {
			dropped = append(dropped, e)
			continue
		}
		if now.Sub(e.lastRetry) >= retryQueueRetryDelay {
			ready = append(ready, e)
		}
		kept = append(kept, e)
	}
	q.entries = kept
	return ready, dropped
}

// Remove removes a specific entry from the queue after successful retry.
func (q *retryQueue) Remove(entry *retryEntry) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, e := range q.entries {
		if e == entry {
			q.entries = append(q.entries[:i], q.entries[i+1:]...)
			return
		}
	}
}

// MarkRetried increments the retry count and updates the last retry time.
func (q *retryQueue) MarkRetried(entry *retryEntry) {
	q.mu.Lock()
	defer q.mu.Unlock()
	entry.retries++
	entry.lastRetry = time.Now()
}

// Start begins the background retry goroutine. retryFn is called for each
// entry ready to retry, lastAttempt=true if this is the final retry attempt.
// Return true from retryFn if the retry succeeded and the entry should be
// removed. droppedFn is called for each entry that is dropped due to max age
// or max retries, so the caller can log and post diagnostics.
func (q *retryQueue) Start(ctx context.Context, retryFn func(e *retryEntry, lastAttempt bool) bool, droppedFn func(e *retryEntry, reason string)) {
	q.mu.Lock()
	q.done = make(chan struct{})
	done := q.done
	q.mu.Unlock()

	go func() {
		defer close(done)
		ticker := time.NewTicker(retryQueueTickRate)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				ready, dropped := q.Drain(now)
				for _, e := range dropped {
					reason := "max_retries"
					if now.Sub(e.enqueuedAt) >= q.maxAge {
						reason = "max_age"
					}
					if droppedFn != nil {
						droppedFn(e, reason)
					}
				}
				for _, e := range ready {
					lastAttempt := e.retries == retryQueueMaxRetries-1
					if retryFn(e, lastAttempt) {
						q.Remove(e)
					} else {
						q.MarkRetried(e)
					}
				}
			}
		}
	}()
}

// Wait blocks until the background retry goroutine exits. Safe to call after
// the context passed to Start has been cancelled; a no-op if Start was never
// called.
func (q *retryQueue) Wait() {
	q.mu.Lock()
	done := q.done
	q.mu.Unlock()
	if done != nil {
		<-done
	}
}
