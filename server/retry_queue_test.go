package main

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryQueue_EnqueueAndLen(t *testing.T) {
	q := newRetryQueue(time.Minute)
	assert.Equal(t, 0, q.Len())

	ok := q.Enqueue("conn", []byte("a"), "rid-1", "post")
	assert.True(t, ok)
	assert.Equal(t, 1, q.Len())

	ok = q.Enqueue("conn", []byte("b"), "rid-2", "post")
	assert.True(t, ok)
	assert.Equal(t, 2, q.Len())
}

func TestRetryQueue_EnqueueFull(t *testing.T) {
	q := newRetryQueue(time.Minute)
	for range retryQueueMaxSize {
		assert.True(t, q.Enqueue("conn", []byte("x"), "rid", "post"))
	}
	assert.Equal(t, retryQueueMaxSize, q.Len())

	// Next enqueue should be rejected.
	assert.False(t, q.Enqueue("conn", []byte("x"), "rid", "post"))
	assert.Equal(t, retryQueueMaxSize, q.Len())
}

func TestRetryQueue_EnqueueCopiesData(t *testing.T) {
	q := newRetryQueue(time.Minute)
	data := []byte("hello")
	q.Enqueue("conn", data, "rid", "post")

	// Mutate caller buffer.
	data[0] = 'X'

	ready, _ := q.Drain(time.Now().Add(retryQueueRetryDelay + time.Second))
	require.Len(t, ready, 1)
	assert.Equal(t, "hello", string(ready[0].rawData))
}

func TestRetryQueue_DrainReturnsReadyAfterDelay(t *testing.T) {
	q := newRetryQueue(time.Minute)
	q.Enqueue("conn", []byte("a"), "rid", "post")

	// Too early.
	ready, dropped := q.Drain(time.Now())
	assert.Len(t, ready, 0)
	assert.Len(t, dropped, 0)
	assert.Equal(t, 1, q.Len())

	// Past retry delay.
	ready, dropped = q.Drain(time.Now().Add(retryQueueRetryDelay + time.Second))
	assert.Len(t, ready, 1)
	assert.Len(t, dropped, 0)
	// Ready entries remain in queue until explicitly removed.
	assert.Equal(t, 1, q.Len())
}

func TestRetryQueue_DrainDropsPastMaxAge(t *testing.T) {
	q := newRetryQueue(100 * time.Millisecond)
	q.Enqueue("conn", []byte("a"), "rid", "post")

	ready, dropped := q.Drain(time.Now().Add(time.Second))
	assert.Len(t, ready, 0)
	require.Len(t, dropped, 1)
	assert.Equal(t, "rid", dropped[0].remoteID)
	assert.Equal(t, 0, q.Len())
}

func TestRetryQueue_DrainDropsAtMaxRetries(t *testing.T) {
	q := newRetryQueue(time.Hour)
	q.Enqueue("conn", []byte("a"), "rid", "post")

	// Simulate reaching max retries.
	q.mu.Lock()
	q.entries[0].retries = retryQueueMaxRetries
	q.mu.Unlock()

	_, dropped := q.Drain(time.Now().Add(retryQueueRetryDelay + time.Second))
	require.Len(t, dropped, 1)
	assert.Equal(t, 0, q.Len())
}

func TestRetryQueue_Remove(t *testing.T) {
	q := newRetryQueue(time.Minute)
	q.Enqueue("conn", []byte("a"), "rid-a", "post")
	q.Enqueue("conn", []byte("b"), "rid-b", "post")

	ready, _ := q.Drain(time.Now().Add(retryQueueRetryDelay + time.Second))
	require.Len(t, ready, 2)

	q.Remove(ready[0])
	assert.Equal(t, 1, q.Len())

	// Second Remove of a removed entry is a no-op.
	q.Remove(ready[0])
	assert.Equal(t, 1, q.Len())
}

func TestRetryQueue_MarkRetried(t *testing.T) {
	q := newRetryQueue(time.Minute)
	q.Enqueue("conn", []byte("a"), "rid", "post")

	ready, _ := q.Drain(time.Now().Add(retryQueueRetryDelay + time.Second))
	require.Len(t, ready, 1)

	before := ready[0].lastRetry
	time.Sleep(5 * time.Millisecond)
	q.MarkRetried(ready[0])

	assert.Equal(t, 1, ready[0].retries)
	assert.True(t, ready[0].lastRetry.After(before))
}

func TestRetryQueue_SetMaxAge(t *testing.T) {
	q := newRetryQueue(time.Hour)
	q.Enqueue("conn", []byte("a"), "rid", "post")

	// Before shortening, entry is not dropped.
	_, dropped := q.Drain(time.Now().Add(time.Minute))
	assert.Len(t, dropped, 0)

	q.SetMaxAge(10 * time.Millisecond)
	_, dropped = q.Drain(time.Now().Add(time.Second))
	assert.Len(t, dropped, 1)
}

func TestRetryQueue_ConcurrentEnqueue(t *testing.T) {
	q := newRetryQueue(time.Minute)

	var wg sync.WaitGroup
	const workers = 10
	const each = 20
	for range workers {
		wg.Go(func() {
			for range each {
				q.Enqueue("conn", []byte("x"), "rid", "post")
			}
		})
	}
	wg.Wait()

	assert.Equal(t, workers*each, q.Len())
}
