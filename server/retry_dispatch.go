package main

import (
	"time"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/model"
)

// computeRetryMaxAge returns the retry queue max age based on the largest
// configured azure-blob flush interval. For NATS-only or Azure Queue-only
// setups, the default max age is used.
func (p *Plugin) computeRetryMaxAge() time.Duration {
	cfg := p.getConfiguration()

	maxFlush := 0
	check := func(conns []ConnectionConfig) {
		for _, c := range conns {
			if c.Provider == ProviderAzureBlob && c.AzureBlob != nil {
				flush := c.AzureBlob.FlushIntervalSeconds
				if flush == 0 {
					flush = defaultAzureBlobFlushIntervalSec
				}
				if flush > maxFlush {
					maxFlush = flush
				}
			}
		}
	}

	if inbound, err := cfg.GetInboundConnections(); err == nil {
		check(inbound)
	}
	if outbound, err := cfg.GetOutboundConnections(); err == nil {
		check(outbound)
	}

	if maxFlush == 0 {
		return retryQueueDefaultMaxAge
	}

	derived := 2*time.Duration(maxFlush)*time.Second + azureBlobBatchPollInterval
	if derived > retryQueueDefaultMaxAge {
		return derived
	}
	return retryQueueDefaultMaxAge
}

// retryInboundMessage reprocesses a retry queue entry by dispatching directly
// to the appropriate handler, bypassing the semaphore. Returns true if the
// retry succeeded (the entry should be removed from the queue).
func (p *Plugin) retryInboundMessage(entry *retryEntry, lastAttempt bool) bool {
	format := model.DetectFormat(entry.rawData)
	env, err := model.Unmarshal(entry.rawData, format)
	if err != nil {
		// The entry was already parseable at ingest time, so an unmarshal
		// failure here signals corruption or a real bug. Route through
		// handleRetryDropped so operators see it in diagnostics instead of
		// dropping the message silently.
		p.handleRetryDropped(entry, retryDropReasonUnmarshalFailed)
		p.API.LogError("Retry: failed to unmarshal", "conn", entry.connName, "error", err.Error())
		return true // drop malformed
	}

	var missing bool
	switch env.Type {
	case model.MessageTypePost:
		if env.PostMessage == nil {
			return true
		}
		missing = p.handleInboundPost(entry.connName, env.PostMessage, lastAttempt)
	case model.MessageTypeUpdate:
		if env.PostMessage == nil {
			return true
		}
		missing = p.handleInboundUpdate(entry.connName, env.PostMessage)
	case model.MessageTypeDelete:
		if env.DeleteMessage == nil {
			return true
		}
		missing = p.handleInboundDelete(entry.connName, env.DeleteMessage)
	case model.MessageTypeReactionAdd:
		if env.ReactionMessage == nil {
			return true
		}
		missing = p.handleInboundReaction(entry.connName, env.ReactionMessage, true)
	case model.MessageTypeReactionRemove:
		if env.ReactionMessage == nil {
			return true
		}
		missing = p.handleInboundReaction(entry.connName, env.ReactionMessage, false)
	default:
		return true // unknown type, drop
	}

	if !missing {
		p.API.LogWarn("Missing message: retry succeeded",
			"conn", entry.connName, "type", env.Type, "remote_post_id", entry.remoteID, "attempt", entry.retries+1)
		return true
	}

	p.API.LogWarn("Missing message: still missing after retry",
		"conn", entry.connName, "type", env.Type, "remote_post_id", entry.remoteID, "attempt", entry.retries+1,
		"next_retry_in", retryQueueRetryDelay)
	return false
}

// Retry drop reasons emitted by handleRetryDropped.
const (
	retryDropReasonMaxAge          = "max_age"
	retryDropReasonMaxRetries      = "max_retries"
	retryDropReasonUnmarshalFailed = "unmarshal_failed"
)

// handleRetryDropped is called when the retry queue drops an entry due to max
// age, max retries, or a malformed payload detected on retry.
func (p *Plugin) handleRetryDropped(entry *retryEntry, reason string) {
	switch reason {
	case retryDropReasonMaxAge:
		p.API.LogError("Missing message: dropped, exceeded max age",
			"conn", entry.connName, "type", entry.msgType, "remote_post_id", entry.remoteID,
			"age", time.Since(entry.enqueuedAt).String())
	case retryDropReasonUnmarshalFailed:
		p.API.LogError("Missing message: dropped, payload unmarshal failed on retry",
			"conn", entry.connName, "type", entry.msgType, "remote_post_id", entry.remoteID)
	default:
		p.API.LogError("Missing message: dropped after max retries",
			"conn", entry.connName, "type", entry.msgType, "remote_post_id", entry.remoteID,
			"attempts", entry.retries)
	}
}

const defaultAzureBlobFlushIntervalSec = 60

// azureBlobBatchPollInterval is the poll interval for the azure-blob batch provider.
// Declared as var so tests can shrink it.
var azureBlobBatchPollInterval = 30 * time.Second
