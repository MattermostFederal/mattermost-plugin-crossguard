package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withFastTick temporarily shrinks retryQueueTickRate for the duration of a test.
func withFastTick(t *testing.T, d time.Duration) {
	t.Helper()
	orig := retryQueueTickRate
	retryQueueTickRate = d
	t.Cleanup(func() { retryQueueTickRate = orig })
}

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

func TestNewRetryQueue_ZeroMaxAge(t *testing.T) {
	q := newRetryQueue(0)
	assert.Equal(t, retryQueueDefaultMaxAge, q.maxAge)

	q = newRetryQueue(-5 * time.Second)
	assert.Equal(t, retryQueueDefaultMaxAge, q.maxAge)
}

func TestRetryQueue_Wait_NoStart(t *testing.T) {
	q := newRetryQueue(time.Minute)
	done := make(chan struct{})
	go func() {
		q.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Wait on fresh queue should be a no-op")
	}
}

func TestRetryQueue_Start_InvokesRetryFnAndRemovesOnSuccess(t *testing.T) {
	withFastTick(t, 5*time.Millisecond)
	q := newRetryQueue(time.Minute)
	q.Enqueue("conn", []byte("a"), "rid", "post")

	// Make entry eligible immediately.
	q.mu.Lock()
	q.entries[0].lastRetry = time.Now().Add(-2 * retryQueueRetryDelay)
	q.mu.Unlock()

	var calls int32
	called := make(chan struct{}, 1)
	retryFn := func(e *retryEntry, lastAttempt bool) bool {
		atomic.AddInt32(&calls, 1)
		select {
		case called <- struct{}{}:
		default:
		}
		return true
	}

	ctx, cancel := context.WithCancel(context.Background())
	q.Start(ctx, retryFn, nil)

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("retryFn not invoked")
	}

	cancel()
	q.Wait()

	assert.GreaterOrEqual(t, atomic.LoadInt32(&calls), int32(1))
	assert.Equal(t, 0, q.Len())
}

func TestRetryQueue_Start_MarkRetriedOnFailure(t *testing.T) {
	withFastTick(t, 5*time.Millisecond)
	q := newRetryQueue(time.Minute)
	q.Enqueue("conn", []byte("a"), "rid", "post")

	q.mu.Lock()
	q.entries[0].lastRetry = time.Now().Add(-2 * retryQueueRetryDelay)
	q.mu.Unlock()

	called := make(chan struct{}, 4)
	retryFn := func(e *retryEntry, lastAttempt bool) bool {
		select {
		case called <- struct{}{}:
		default:
		}
		// Reset lastRetry far in the past so it's eligible on each tick.
		e.lastRetry = time.Now().Add(-2 * retryQueueRetryDelay)
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	q.Start(ctx, retryFn, nil)

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("retryFn not invoked")
	}

	// Give it a moment to increment retries.
	time.Sleep(30 * time.Millisecond)
	cancel()
	q.Wait()

	q.mu.Lock()
	defer q.mu.Unlock()
	// Entry may have been dropped after max retries, or still present with retries > 0.
	if len(q.entries) > 0 {
		assert.Greater(t, q.entries[0].retries, 0)
	}
}

func TestRetryQueue_Start_DroppedMaxAgeCallsDroppedFn(t *testing.T) {
	withFastTick(t, 5*time.Millisecond)
	q := newRetryQueue(10 * time.Millisecond)
	q.Enqueue("conn", []byte("a"), "rid", "post")

	// Force entry to look old.
	q.mu.Lock()
	q.entries[0].enqueuedAt = time.Now().Add(-time.Hour)
	q.mu.Unlock()

	dropped := make(chan string, 1)
	retryFn := func(e *retryEntry, lastAttempt bool) bool { return true }
	droppedFn := func(e *retryEntry, reason string) {
		select {
		case dropped <- reason:
		default:
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	q.Start(ctx, retryFn, droppedFn)

	select {
	case reason := <-dropped:
		assert.Equal(t, "max_age", reason)
	case <-time.After(time.Second):
		t.Fatal("droppedFn not invoked")
	}

	cancel()
	q.Wait()
}

func TestRetryQueue_Start_DroppedMaxRetriesCallsDroppedFn(t *testing.T) {
	withFastTick(t, 5*time.Millisecond)
	q := newRetryQueue(time.Hour)
	q.Enqueue("conn", []byte("a"), "rid", "post")

	q.mu.Lock()
	q.entries[0].retries = retryQueueMaxRetries
	q.mu.Unlock()

	dropped := make(chan string, 1)
	retryFn := func(e *retryEntry, lastAttempt bool) bool { return true }
	droppedFn := func(e *retryEntry, reason string) {
		select {
		case dropped <- reason:
		default:
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	q.Start(ctx, retryFn, droppedFn)

	select {
	case reason := <-dropped:
		assert.Equal(t, "max_retries", reason)
	case <-time.After(time.Second):
		t.Fatal("droppedFn not invoked")
	}

	cancel()
	q.Wait()
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
