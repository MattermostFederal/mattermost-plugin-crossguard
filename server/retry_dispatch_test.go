package main

import (
	"encoding/json"
	"testing"
	"time"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/model"
)

// stubLogs registers loose matchers that silently accept any LogWarn/LogError/LogDebug/LogInfo call.
// Use in tests that don't care about log contents.
func stubLogs(api *plugintest.API) {
	registerLogMocks(api, "LogWarn", "LogError", "LogDebug", "LogInfo")
}

func mustMarshalEnv(t *testing.T, env *model.Envelope) []byte {
	t.Helper()
	data, err := model.Marshal(env, model.FormatJSON)
	require.NoError(t, err)
	return data
}

func mustMarshalJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return string(data)
}

func TestComputeRetryMaxAge(t *testing.T) {
	newPluginWithConfig := func(inbound, outbound []ConnectionConfig) *Plugin {
		p := &Plugin{}
		cfg := &configuration{}
		if inbound != nil {
			cfg.InboundConnections = mustMarshalJSON(t, inbound)
		}
		if outbound != nil {
			cfg.OutboundConnections = mustMarshalJSON(t, outbound)
		}
		p.setConfiguration(cfg)
		return p
	}

	t.Run("no azure-blob connections returns default", func(t *testing.T) {
		p := newPluginWithConfig(nil, nil)
		assert.Equal(t, retryQueueDefaultMaxAge, p.computeRetryMaxAge())
	})

	t.Run("nats-only returns default", func(t *testing.T) {
		p := newPluginWithConfig([]ConnectionConfig{
			{Name: "n1", Provider: ProviderNATS, NATS: &NATSProviderConfig{Address: "nats://x"}},
		}, nil)
		assert.Equal(t, retryQueueDefaultMaxAge, p.computeRetryMaxAge())
	})

	t.Run("azure-blob with zero flush uses default flush interval", func(t *testing.T) {
		p := newPluginWithConfig([]ConnectionConfig{
			{Name: "b1", Provider: ProviderAzureBlob, AzureBlob: &AzureBlobProviderConfig{
				ServiceURL: "http://x", AccountName: "a", AccountKey: "a", BlobContainerName: "c",
			}},
		}, nil)
		// derived = 2*60s + 30s = 150s, which is greater than default (120s)
		want := 2*time.Duration(defaultAzureBlobFlushIntervalSec)*time.Second + azureBlobBatchPollInterval
		assert.Equal(t, want, p.computeRetryMaxAge())
	})

	t.Run("azure-blob with large flush overrides default", func(t *testing.T) {
		p := newPluginWithConfig(nil, []ConnectionConfig{
			{Name: "b1", Provider: ProviderAzureBlob, AzureBlob: &AzureBlobProviderConfig{
				ServiceURL: "http://x", AccountName: "a", AccountKey: "a", BlobContainerName: "c",
				FlushIntervalSeconds: 300,
			}},
		})
		want := 2*300*time.Second + azureBlobBatchPollInterval
		assert.Equal(t, want, p.computeRetryMaxAge())
	})

	t.Run("largest flush wins across inbound and outbound", func(t *testing.T) {
		p := newPluginWithConfig(
			[]ConnectionConfig{
				{Name: "i", Provider: ProviderAzureBlob, AzureBlob: &AzureBlobProviderConfig{
					ServiceURL: "http://x", AccountName: "a", AccountKey: "a", BlobContainerName: "c",
					FlushIntervalSeconds: 60,
				}},
			},
			[]ConnectionConfig{
				{Name: "o", Provider: ProviderAzureBlob, AzureBlob: &AzureBlobProviderConfig{
					ServiceURL: "http://x", AccountName: "a", AccountKey: "a", BlobContainerName: "c",
					FlushIntervalSeconds: 200,
				}},
			},
		)
		want := 2*200*time.Second + azureBlobBatchPollInterval
		assert.Equal(t, want, p.computeRetryMaxAge())
	})

	t.Run("invalid inbound JSON falls back gracefully", func(t *testing.T) {
		p := &Plugin{}
		cfg := &configuration{InboundConnections: "not-json"}
		p.setConfiguration(cfg)
		assert.Equal(t, retryQueueDefaultMaxAge, p.computeRetryMaxAge())
	})
}

func TestRetryInboundMessage(t *testing.T) {
	setup := func() (*plugintest.API, *Plugin, *testKVStore) {
		api := &plugintest.API{}
		stubLogs(api)
		p, kvs := setupTestPlugin(api)
		return api, p, kvs
	}

	t.Run("malformed JSON is dropped", func(t *testing.T) {
		_, p, _ := setup()
		entry := &retryEntry{connName: "high", rawData: []byte("{not json")}
		assert.True(t, p.retryInboundMessage(entry, false))
	})

	t.Run("unknown message type is dropped", func(t *testing.T) {
		_, p, _ := setup()
		data := mustMarshalEnv(t, &model.Envelope{Type: "crossguard_unknown"})
		assert.True(t, p.retryInboundMessage(&retryEntry{connName: "high", rawData: data}, false))
	})

	t.Run("post with nil PostMessage is dropped", func(t *testing.T) {
		_, p, _ := setup()
		data := mustMarshalEnv(t, &model.Envelope{Type: model.MessageTypePost})
		assert.True(t, p.retryInboundMessage(&retryEntry{connName: "high", rawData: data}, false))
	})

	t.Run("update with nil PostMessage is dropped", func(t *testing.T) {
		_, p, _ := setup()
		data := mustMarshalEnv(t, &model.Envelope{Type: model.MessageTypeUpdate})
		assert.True(t, p.retryInboundMessage(&retryEntry{connName: "high", rawData: data}, false))
	})

	t.Run("delete with nil DeleteMessage is dropped", func(t *testing.T) {
		_, p, _ := setup()
		data := mustMarshalEnv(t, &model.Envelope{Type: model.MessageTypeDelete})
		assert.True(t, p.retryInboundMessage(&retryEntry{connName: "high", rawData: data}, false))
	})

	t.Run("reaction add with nil ReactionMessage is dropped", func(t *testing.T) {
		_, p, _ := setup()
		data := mustMarshalEnv(t, &model.Envelope{Type: model.MessageTypeReactionAdd})
		assert.True(t, p.retryInboundMessage(&retryEntry{connName: "high", rawData: data}, false))
	})

	t.Run("reaction remove with nil ReactionMessage is dropped", func(t *testing.T) {
		_, p, _ := setup()
		data := mustMarshalEnv(t, &model.Envelope{Type: model.MessageTypeReactionRemove})
		assert.True(t, p.retryInboundMessage(&retryEntry{connName: "high", rawData: data}, false))
	})

	t.Run("update returns false when mapping absent (still missing)", func(t *testing.T) {
		_, p, _ := setup()
		data := mustMarshalEnv(t, &model.Envelope{
			Type:        model.MessageTypeUpdate,
			PostMessage: &model.PostMessage{PostID: "remote-1", MessageText: "x"},
		})
		entry := &retryEntry{connName: "high", rawData: data, remoteID: "remote-1"}
		assert.False(t, p.retryInboundMessage(entry, false))
	})

	t.Run("update returns true when mapping present (success)", func(t *testing.T) {
		api, p, kvs := setup()
		existing := &mmModel.Post{Id: "local-1", Message: "old"}
		api.On("GetPost", "local-1").Return(existing, nil)
		api.On("UpdatePost", mock.Anything).Return(existing, nil)

		require.NoError(t, kvs.SetPostMapping("high", "remote-1", "local-1"))
		data := mustMarshalEnv(t, &model.Envelope{
			Type:        model.MessageTypeUpdate,
			PostMessage: &model.PostMessage{PostID: "remote-1", MessageText: "new"},
		})
		entry := &retryEntry{connName: "high", rawData: data, remoteID: "remote-1"}
		assert.True(t, p.retryInboundMessage(entry, false))
	})

	t.Run("delete returns false when mapping missing", func(t *testing.T) {
		_, p, _ := setup()
		data := mustMarshalEnv(t, &model.Envelope{
			Type:          model.MessageTypeDelete,
			DeleteMessage: &model.DeleteMessage{PostID: "missing-id"},
		})
		assert.False(t, p.retryInboundMessage(&retryEntry{connName: "high", rawData: data, remoteID: "missing-id"}, false))
	})

	t.Run("reaction add returns false when mapping missing", func(t *testing.T) {
		_, p, _ := setup()
		data := mustMarshalEnv(t, &model.Envelope{
			Type:            model.MessageTypeReactionAdd,
			ReactionMessage: &model.ReactionMessage{PostID: "missing"},
		})
		assert.False(t, p.retryInboundMessage(&retryEntry{connName: "high", rawData: data, remoteID: "missing"}, false))
	})

	t.Run("reaction remove returns false when mapping missing", func(t *testing.T) {
		_, p, _ := setup()
		data := mustMarshalEnv(t, &model.Envelope{
			Type:            model.MessageTypeReactionRemove,
			ReactionMessage: &model.ReactionMessage{PostID: "missing"},
		})
		assert.False(t, p.retryInboundMessage(&retryEntry{connName: "high", rawData: data, remoteID: "missing"}, false))
	})
}

func TestHandleRetryDropped(t *testing.T) {
	t.Run("max_age logs exceeded max age", func(t *testing.T) {
		api := &plugintest.API{}
		stubLogs(api)

		p := &Plugin{}
		p.SetAPI(api)

		entry := &retryEntry{
			connName:   "high",
			msgType:    "post",
			remoteID:   "r-1",
			enqueuedAt: time.Now().Add(-5 * time.Minute),
		}
		p.handleRetryDropped(entry, "max_age")
	})

	t.Run("other reason logs after max retries", func(t *testing.T) {
		api := &plugintest.API{}
		stubLogs(api)

		p := &Plugin{}
		p.SetAPI(api)

		entry := &retryEntry{
			connName: "high",
			msgType:  "post",
			remoteID: "r-2",
			retries:  3,
		}
		p.handleRetryDropped(entry, "max_retries")
	})
}
