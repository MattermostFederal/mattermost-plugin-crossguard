package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/model"
	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

func hooksLogMocks(api *plugintest.API) {
	registerLogMocks(api, "LogInfo", "LogWarn", "LogError", "LogDebug")
}

// ---- isChannelRelayEnabled ----

func TestIsChannelRelayEnabled(t *testing.T) {
	t.Run("no channel connections returns nil", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return nil, nil
		}

		ch, team, conns := p.isChannelRelayEnabled("chan-id")
		assert.Nil(t, ch)
		assert.Nil(t, team)
		assert.Nil(t, conns)
	})

	t.Run("GetChannelConnections error returns nil", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return nil, errors.New("store error")
		}

		ch, team, conns := p.isChannelRelayEnabled("chan-id")
		assert.Nil(t, ch)
		assert.Nil(t, team)
		assert.Nil(t, conns)
	})

	t.Run("GetChannel error returns nil", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		// Default kvs returns connections, so GetChannelConnections succeeds.
		api.On("GetChannel", "chan-id").Return(nil, &mmModel.AppError{Message: "not found"})

		ch, team, conns := p.isChannelRelayEnabled("chan-id")
		assert.Nil(t, ch)
		assert.Nil(t, team)
		assert.Nil(t, conns)
	})

	t.Run("no team connections returns nil", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)

		channel := &mmModel.Channel{Id: "chan-id", Name: "ch", TeamId: "team-id"}
		api.On("GetChannel", "chan-id").Return(channel, nil)
		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return nil, nil
		}

		ch, team, conns := p.isChannelRelayEnabled("chan-id")
		assert.Nil(t, ch)
		assert.Nil(t, team)
		assert.Nil(t, conns)
	})

	t.Run("GetTeamConnections error returns nil", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)

		channel := &mmModel.Channel{Id: "chan-id", Name: "ch", TeamId: "team-id"}
		api.On("GetChannel", "chan-id").Return(channel, nil)
		kvs.getTeamConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return nil, errors.New("team store error")
		}

		ch, team, conns := p.isChannelRelayEnabled("chan-id")
		assert.Nil(t, ch)
		assert.Nil(t, team)
		assert.Nil(t, conns)
	})

	t.Run("GetTeam error returns nil", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		channel := &mmModel.Channel{Id: "chan-id", Name: "ch", TeamId: "team-id"}
		api.On("GetChannel", "chan-id").Return(channel, nil)
		api.On("GetTeam", "team-id").Return(nil, &mmModel.AppError{Message: "team not found"})

		ch, team, conns := p.isChannelRelayEnabled("chan-id")
		assert.Nil(t, ch)
		assert.Nil(t, team)
		assert.Nil(t, conns)
	})

	t.Run("happy path returns channel team and connections", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		channel := &mmModel.Channel{Id: "chan-id", Name: "ch", TeamId: "team-id"}
		team := &mmModel.Team{Id: "team-id", Name: "test-team"}
		api.On("GetChannel", "chan-id").Return(channel, nil)
		api.On("GetTeam", "team-id").Return(team, nil)

		gotCh, gotTeam, gotConns := p.isChannelRelayEnabled("chan-id")
		assert.NotNil(t, gotCh)
		assert.Equal(t, "chan-id", gotCh.Id)
		assert.NotNil(t, gotTeam)
		assert.Equal(t, "team-id", gotTeam.Id)
		assert.NotEmpty(t, gotConns)
	})
}

// ---- MessageHasBeenPosted ----

func TestMessageHasBeenPosted(t *testing.T) {
	t.Run("system message is ignored", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		post := &mmModel.Post{Id: "p1", Type: mmModel.PostTypeJoinChannel}
		p.MessageHasBeenPosted(nil, post)
		// No API calls expected beyond log mocks.
	})

	t.Run("bot own message is ignored", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		post := &mmModel.Post{Id: "p1", UserId: "bot-user-id"}
		p.MessageHasBeenPosted(nil, post)
	})

	t.Run("relayed message is ignored", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		post := &mmModel.Post{Id: "p1", UserId: "user-id"}
		post.AddProp("crossguard_relayed", true)
		p.MessageHasBeenPosted(nil, post)
	})

	t.Run("relay not enabled is no-op", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return nil, nil
		}

		post := &mmModel.Post{Id: "p1", ChannelId: "chan-id", UserId: "user-id", Message: "hello"}
		p.MessageHasBeenPosted(nil, post)
	})

	t.Run("GetUser failure is no-op", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		channel := &mmModel.Channel{Id: "chan-id", Name: "ch", TeamId: "team-id"}
		team := &mmModel.Team{Id: "team-id", Name: "test-team"}
		api.On("GetChannel", "chan-id").Return(channel, nil)
		api.On("GetTeam", "team-id").Return(team, nil)
		api.On("GetUser", "user-id").Return(nil, &mmModel.AppError{Message: "user not found"})

		post := &mmModel.Post{Id: "p1", ChannelId: "chan-id", UserId: "user-id", Message: "hello"}
		p.MessageHasBeenPosted(nil, post)
	})

	t.Run("happy path relays post", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		var published atomic.Bool
		p.outboundConns = []outboundConn{{
			provider: &mockQueueProvider{publishFn: func(_ context.Context, _ []byte) error {
				published.Store(true)
				return nil
			}},
			name:          "high",
			healthy:       true,
			lastCheckTime: time.Now(),
		}}

		channel := &mmModel.Channel{Id: "chan-id", Name: "ch", TeamId: "team-id"}
		team := &mmModel.Team{Id: "team-id", Name: "test-team"}
		user := &mmModel.User{Id: "user-id", Username: "alice"}

		api.On("GetChannel", "chan-id").Return(channel, nil)
		api.On("GetTeam", "team-id").Return(team, nil)
		api.On("GetUser", "user-id").Return(user, nil)

		post := &mmModel.Post{Id: "p1", ChannelId: "chan-id", UserId: "user-id", Message: "hello"}
		p.MessageHasBeenPosted(nil, post)
		p.wg.Wait()

		assert.True(t, published.Load())
	})

	t.Run("post with file IDs does not panic", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		var published atomic.Bool
		p.outboundConns = []outboundConn{{
			provider: &mockQueueProvider{
				publishFn: func(_ context.Context, _ []byte) error {
					published.Store(true)
					return nil
				},
			},
			name:                "high",
			healthy:             true,
			lastCheckTime:       time.Now(),
			fileTransferEnabled: false,
		}}

		channel := &mmModel.Channel{Id: "chan-id", Name: "ch", TeamId: "team-id"}
		team := &mmModel.Team{Id: "team-id", Name: "test-team"}
		user := &mmModel.User{Id: "user-id", Username: "alice"}

		api.On("GetChannel", "chan-id").Return(channel, nil)
		api.On("GetTeam", "team-id").Return(team, nil)
		api.On("GetUser", "user-id").Return(user, nil)
		api.On("GetConfig").Return(&mmModel.Config{}).Maybe()

		post := &mmModel.Post{
			Id:        "p1",
			ChannelId: "chan-id",
			UserId:    "user-id",
			Message:   "hello with file",
			FileIds:   mmModel.StringArray{"file-id-1"},
		}
		p.MessageHasBeenPosted(nil, post)
		p.wg.Wait()

		assert.True(t, published.Load())
	})
}

// ---- MessageHasBeenUpdated ----

func TestMessageHasBeenUpdated(t *testing.T) {
	t.Run("system message is ignored", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		post := &mmModel.Post{Id: "p1", Type: mmModel.PostTypeJoinChannel}
		p.MessageHasBeenUpdated(nil, post, post)
	})

	t.Run("bot own message is ignored", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		post := &mmModel.Post{Id: "p1", UserId: "bot-user-id"}
		p.MessageHasBeenUpdated(nil, post, post)
	})

	t.Run("relayed message is ignored", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		post := &mmModel.Post{Id: "p1", UserId: "user-id"}
		post.AddProp("crossguard_relayed", true)
		p.MessageHasBeenUpdated(nil, post, post)
	})

	t.Run("relay not enabled is no-op", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return nil, nil
		}

		post := &mmModel.Post{Id: "p1", ChannelId: "chan-id", UserId: "user-id", Message: "edited"}
		p.MessageHasBeenUpdated(nil, post, post)
	})

	t.Run("GetUser failure is no-op", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		channel := &mmModel.Channel{Id: "chan-id", Name: "ch", TeamId: "team-id"}
		team := &mmModel.Team{Id: "team-id", Name: "test-team"}
		api.On("GetChannel", "chan-id").Return(channel, nil)
		api.On("GetTeam", "team-id").Return(team, nil)
		api.On("GetUser", "user-id").Return(nil, &mmModel.AppError{Message: "user not found"})

		post := &mmModel.Post{Id: "p1", ChannelId: "chan-id", UserId: "user-id", Message: "edited"}
		p.MessageHasBeenUpdated(nil, post, post)
	})

	t.Run("happy path relays update", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		var published atomic.Bool
		p.outboundConns = []outboundConn{{
			provider: &mockQueueProvider{publishFn: func(_ context.Context, _ []byte) error {
				published.Store(true)
				return nil
			}},
			name:          "high",
			healthy:       true,
			lastCheckTime: time.Now(),
		}}

		channel := &mmModel.Channel{Id: "chan-id", Name: "ch", TeamId: "team-id"}
		team := &mmModel.Team{Id: "team-id", Name: "test-team"}
		user := &mmModel.User{Id: "user-id", Username: "alice"}

		api.On("GetChannel", "chan-id").Return(channel, nil)
		api.On("GetTeam", "team-id").Return(team, nil)
		api.On("GetUser", "user-id").Return(user, nil)

		newPost := &mmModel.Post{Id: "p1", ChannelId: "chan-id", UserId: "user-id", Message: "edited"}
		oldPost := &mmModel.Post{Id: "p1", ChannelId: "chan-id", UserId: "user-id", Message: "original"}
		p.MessageHasBeenUpdated(nil, newPost, oldPost)
		p.wg.Wait()

		assert.True(t, published.Load())
	})
}

// ---- MessageHasBeenDeleted ----

func TestMessageHasBeenDeleted(t *testing.T) {
	t.Run("bot message is ignored", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		post := &mmModel.Post{Id: "p1", UserId: "bot-user-id"}
		p.MessageHasBeenDeleted(nil, post)
	})

	t.Run("deleting flag set is ignored", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		kvs.isDeletingFlagSetFn = func(string) (bool, error) {
			return true, nil
		}

		post := &mmModel.Post{Id: "p1", UserId: "user-id"}
		p.MessageHasBeenDeleted(nil, post)
	})

	t.Run("IsDeletingFlagSet error skips relay", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		kvs.isDeletingFlagSetFn = func(string) (bool, error) {
			return false, errors.New("kv error")
		}

		post := &mmModel.Post{Id: "p1", UserId: "user-id"}
		p.MessageHasBeenDeleted(nil, post)
	})

	t.Run("relay not enabled is no-op", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return nil, nil
		}

		post := &mmModel.Post{Id: "p1", ChannelId: "chan-id", UserId: "user-id"}
		p.MessageHasBeenDeleted(nil, post)
	})

	t.Run("happy path relays delete", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		var published atomic.Bool
		p.outboundConns = []outboundConn{{
			provider: &mockQueueProvider{publishFn: func(_ context.Context, _ []byte) error {
				published.Store(true)
				return nil
			}},
			name:          "high",
			healthy:       true,
			lastCheckTime: time.Now(),
		}}

		channel := &mmModel.Channel{Id: "chan-id", Name: "ch", TeamId: "team-id"}
		team := &mmModel.Team{Id: "team-id", Name: "test-team"}

		api.On("GetChannel", "chan-id").Return(channel, nil)
		api.On("GetTeam", "team-id").Return(team, nil)

		post := &mmModel.Post{Id: "p1", ChannelId: "chan-id", UserId: "user-id"}
		p.MessageHasBeenDeleted(nil, post)
		p.wg.Wait()

		assert.True(t, published.Load())
	})
}

// ---- ReactionHasBeenAdded ----

func TestReactionHasBeenAdded(t *testing.T) {
	t.Run("GetPost failure is no-op", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		api.On("GetPost", "post-id").Return(nil, &mmModel.AppError{Message: "not found"})

		reaction := &mmModel.Reaction{PostId: "post-id", UserId: "user-id", EmojiName: "thumbsup"}
		p.ReactionHasBeenAdded(nil, reaction)
	})

	t.Run("system post is ignored", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		post := &mmModel.Post{Id: "post-id", Type: mmModel.PostTypeJoinChannel, UserId: "user-id"}
		api.On("GetPost", "post-id").Return(post, nil)

		reaction := &mmModel.Reaction{PostId: "post-id", UserId: "reactor-id", EmojiName: "thumbsup"}
		p.ReactionHasBeenAdded(nil, reaction)
	})

	t.Run("bot post is ignored", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		post := &mmModel.Post{Id: "post-id", UserId: "bot-user-id"}
		api.On("GetPost", "post-id").Return(post, nil)

		reaction := &mmModel.Reaction{PostId: "post-id", UserId: "reactor-id", EmojiName: "thumbsup"}
		p.ReactionHasBeenAdded(nil, reaction)
	})

	t.Run("GetUser failure is no-op", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		post := &mmModel.Post{Id: "post-id", UserId: "user-id", ChannelId: "chan-id"}
		api.On("GetPost", "post-id").Return(post, nil)
		api.On("GetUser", "reactor-id").Return(nil, &mmModel.AppError{Message: "user not found"})

		reaction := &mmModel.Reaction{PostId: "post-id", UserId: "reactor-id", EmojiName: "thumbsup"}
		p.ReactionHasBeenAdded(nil, reaction)
	})

	t.Run("sync user is ignored", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		post := &mmModel.Post{Id: "post-id", UserId: "user-id", ChannelId: "chan-id"}
		syncUser := &mmModel.User{Id: "reactor-id", Username: "alice.high", Position: "crossguard-sync"}

		api.On("GetPost", "post-id").Return(post, nil)
		api.On("GetUser", "reactor-id").Return(syncUser, nil)

		reaction := &mmModel.Reaction{PostId: "post-id", UserId: "reactor-id", EmojiName: "thumbsup"}
		p.ReactionHasBeenAdded(nil, reaction)
	})

	t.Run("relay not enabled is no-op", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)

		post := &mmModel.Post{Id: "post-id", UserId: "user-id", ChannelId: "chan-id"}
		user := &mmModel.User{Id: "reactor-id", Username: "alice"}

		api.On("GetPost", "post-id").Return(post, nil)
		api.On("GetUser", "reactor-id").Return(user, nil)

		kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
			return nil, nil
		}

		reaction := &mmModel.Reaction{PostId: "post-id", UserId: "reactor-id", EmojiName: "thumbsup"}
		p.ReactionHasBeenAdded(nil, reaction)
	})

	t.Run("happy path relays reaction add", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		var published atomic.Bool
		p.outboundConns = []outboundConn{{
			provider: &mockQueueProvider{publishFn: func(_ context.Context, _ []byte) error {
				published.Store(true)
				return nil
			}},
			name:          "high",
			healthy:       true,
			lastCheckTime: time.Now(),
		}}

		post := &mmModel.Post{Id: "post-id", UserId: "user-id", ChannelId: "chan-id"}
		channel := &mmModel.Channel{Id: "chan-id", Name: "ch", TeamId: "team-id"}
		team := &mmModel.Team{Id: "team-id", Name: "test-team"}
		user := &mmModel.User{Id: "reactor-id", Username: "alice"}

		api.On("GetPost", "post-id").Return(post, nil)
		api.On("GetUser", "reactor-id").Return(user, nil)
		api.On("GetChannel", "chan-id").Return(channel, nil)
		api.On("GetTeam", "team-id").Return(team, nil)

		reaction := &mmModel.Reaction{PostId: "post-id", UserId: "reactor-id", EmojiName: "thumbsup"}
		p.ReactionHasBeenAdded(nil, reaction)
		p.wg.Wait()

		assert.True(t, published.Load())
	})
}

// ---- ReactionHasBeenRemoved ----

func TestReactionHasBeenRemoved(t *testing.T) {
	t.Run("GetPost failure is no-op", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		api.On("GetPost", "post-id").Return(nil, &mmModel.AppError{Message: "not found"})

		reaction := &mmModel.Reaction{PostId: "post-id", UserId: "user-id", EmojiName: "thumbsup"}
		p.ReactionHasBeenRemoved(nil, reaction)
	})

	t.Run("system post is ignored", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		post := &mmModel.Post{Id: "post-id", Type: mmModel.PostTypeJoinChannel, UserId: "user-id"}
		api.On("GetPost", "post-id").Return(post, nil)

		reaction := &mmModel.Reaction{PostId: "post-id", UserId: "reactor-id", EmojiName: "thumbsup"}
		p.ReactionHasBeenRemoved(nil, reaction)
	})

	t.Run("bot post is ignored", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		post := &mmModel.Post{Id: "post-id", UserId: "bot-user-id"}
		api.On("GetPost", "post-id").Return(post, nil)

		reaction := &mmModel.Reaction{PostId: "post-id", UserId: "reactor-id", EmojiName: "thumbsup"}
		p.ReactionHasBeenRemoved(nil, reaction)
	})

	t.Run("sync user is ignored", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		post := &mmModel.Post{Id: "post-id", UserId: "user-id", ChannelId: "chan-id"}
		syncUser := &mmModel.User{Id: "reactor-id", Username: "alice.high", Position: "crossguard-sync"}

		api.On("GetPost", "post-id").Return(post, nil)
		api.On("GetUser", "reactor-id").Return(syncUser, nil)

		reaction := &mmModel.Reaction{PostId: "post-id", UserId: "reactor-id", EmojiName: "thumbsup"}
		p.ReactionHasBeenRemoved(nil, reaction)
	})

	t.Run("happy path relays reaction remove", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		var published atomic.Bool
		p.outboundConns = []outboundConn{{
			provider: &mockQueueProvider{publishFn: func(_ context.Context, _ []byte) error {
				published.Store(true)
				return nil
			}},
			name:          "high",
			healthy:       true,
			lastCheckTime: time.Now(),
		}}

		post := &mmModel.Post{Id: "post-id", UserId: "user-id", ChannelId: "chan-id"}
		channel := &mmModel.Channel{Id: "chan-id", Name: "ch", TeamId: "team-id"}
		team := &mmModel.Team{Id: "team-id", Name: "test-team"}
		user := &mmModel.User{Id: "reactor-id", Username: "alice"}

		api.On("GetPost", "post-id").Return(post, nil)
		api.On("GetUser", "reactor-id").Return(user, nil)
		api.On("GetChannel", "chan-id").Return(channel, nil)
		api.On("GetTeam", "team-id").Return(team, nil)

		reaction := &mmModel.Reaction{PostId: "post-id", UserId: "reactor-id", EmojiName: "thumbsup"}
		p.ReactionHasBeenRemoved(nil, reaction)
		p.wg.Wait()

		assert.True(t, published.Load())
	})
}

// ---- relayToOutbound ----

func TestRelayToOutbound(t *testing.T) {
	t.Run("semaphore full drops event", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		// Use a capacity-1 semaphore and fill it.
		p.relaySem = make(chan struct{}, 1)
		p.relaySem <- struct{}{}

		p.outboundConns = []outboundConn{{
			provider: &mockQueueProvider{publishFn: func(_ context.Context, _ []byte) error {
				t.Fatal("Publish should not be called when semaphore is full")
				return nil
			}},
			name:          "high",
			healthy:       true,
			lastCheckTime: time.Now(),
		}}

		env := &model.Envelope{}
		conns := []store.TeamConnection{{Direction: "outbound", Connection: "high"}}
		p.relayToOutbound(env, conns, "test-context")

		// Drain the semaphore so the test can clean up.
		<-p.relaySem
	})

	t.Run("successful relay acquires and releases semaphore", func(t *testing.T) {
		api := &plugintest.API{}
		hooksLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{}

		var published atomic.Bool
		p.outboundConns = []outboundConn{{
			provider: &mockQueueProvider{publishFn: func(_ context.Context, _ []byte) error {
				published.Store(true)
				return nil
			}},
			name:          "high",
			healthy:       true,
			lastCheckTime: time.Now(),
		}}

		env := &model.Envelope{}
		conns := []store.TeamConnection{{Direction: "outbound", Connection: "high"}}
		p.relayToOutbound(env, conns, "test-context")
		p.wg.Wait()

		assert.True(t, published.Load())
		// Semaphore should be released (channel should be empty).
		assert.Equal(t, 0, len(p.relaySem))
	})
}

// ---------------------------------------------------------------------------
// Additional hook edge-case tests
// ---------------------------------------------------------------------------

func TestMessageHasBeenPosted_BotUserFiltered(t *testing.T) {
	api := &plugintest.API{}
	hooksLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)
	p.configuration = defaultTestConfig()

	post := &mmModel.Post{
		Id:        "post1",
		UserId:    p.botUserID, // bot post should be skipped
		ChannelId: "chan1",
		Message:   "hello",
	}
	// No KV calls expected since bot is filtered early
	p.MessageHasBeenPosted(nil, post)
}

func TestMessageHasBeenPosted_RelayedPostFiltered(t *testing.T) {
	api := &plugintest.API{}
	hooksLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)
	p.configuration = defaultTestConfig()

	post := &mmModel.Post{
		Id:        "post1",
		UserId:    "user1",
		ChannelId: "chan1",
		Message:   "hello",
	}
	post.AddProp("crossguard_relayed", true)
	// No KV calls expected since relayed post is filtered
	p.MessageHasBeenPosted(nil, post)
}

func TestMessageHasBeenUpdated_BotUserFiltered(t *testing.T) {
	api := &plugintest.API{}
	hooksLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)
	p.configuration = defaultTestConfig()

	post := &mmModel.Post{
		Id:        "post1",
		UserId:    p.botUserID,
		ChannelId: "chan1",
		Message:   "updated",
	}
	p.MessageHasBeenUpdated(nil, post, post)
}

func TestMessageHasBeenDeleted_DeletingFlagTrue(t *testing.T) {
	api := &plugintest.API{}
	hooksLogMocks(api)
	p, kvs := setupTestPluginWithRouter(api)
	p.configuration = defaultTestConfig()

	kvs.isDeletingFlagSetFn = func(postID string) (bool, error) {
		return true, nil
	}

	post := &mmModel.Post{
		Id:        "post1",
		UserId:    "user1",
		ChannelId: "chan1",
	}
	// Should skip since deleting flag is set (prevents relay loops)
	p.MessageHasBeenDeleted(nil, post)
}

func TestRelayToOutbound_EmptyConnNames(t *testing.T) {
	api := &plugintest.API{}
	hooksLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)
	p.configuration = defaultTestConfig()

	env := &model.Envelope{}
	// Empty slice means no connections to relay to
	p.relayToOutbound(env, []store.TeamConnection{}, "test")
	p.wg.Wait()
	// Semaphore should be released
	assert.Equal(t, 0, len(p.relaySem))
}

func TestReactionHasBeenRemoved_GetUserFailure(t *testing.T) {
	api := &plugintest.API{}
	hooksLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)

	normalPost := &mmModel.Post{
		Id:        "post-1",
		UserId:    "user-1",
		ChannelId: "ch1",
	}
	api.On("GetPost", "post-1").Return(normalPost, nil)
	api.On("GetUser", "user-1").Return(nil, &mmModel.AppError{Message: "user not found"})

	reaction := &mmModel.Reaction{
		UserId:    "user-1",
		PostId:    "post-1",
		EmojiName: "thumbsup",
	}

	p.ReactionHasBeenRemoved(nil, reaction)
	p.wg.Wait()

	api.AssertNotCalled(t, "RemoveReaction", mock.Anything)
}

func TestMessageHasBeenDeleted_IsDeletingFlagError(t *testing.T) {
	api := &plugintest.API{}
	hooksLogMocks(api)
	p, kvs := setupTestPluginWithRouter(api)

	kvs.isDeletingFlagSetFn = func(postID string) (bool, error) {
		return false, errors.New("kv failure")
	}

	post := &mmModel.Post{
		Id:        "post-1",
		UserId:    "user-1",
		ChannelId: "ch1",
	}

	p.MessageHasBeenDeleted(nil, post)
	p.wg.Wait()

	// Should log error and return without relaying.
	api.AssertNotCalled(t, "GetChannel", mock.Anything)
}

// ---------------------------------------------------------------------------
// Additional hook edge-case tests (new)
// ---------------------------------------------------------------------------

func TestMessageHasBeenPosted_SystemMessage(t *testing.T) {
	api := &plugintest.API{}
	hooksLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)

	post := &mmModel.Post{Id: "p1", Type: "system_join_channel", UserId: "user-id"}
	p.MessageHasBeenPosted(nil, post)
	// No channel/team lookups expected for system messages.
	api.AssertNotCalled(t, "GetChannel", mock.Anything)
	api.AssertNotCalled(t, "GetUser", mock.Anything)
}

func TestMessageHasBeenPosted_WithFiles(t *testing.T) {
	api := &plugintest.API{}
	hooksLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)
	p.configuration = &configuration{}

	var published atomic.Bool
	p.outboundConns = []outboundConn{{
		provider: &mockQueueProvider{
			publishFn: func(_ context.Context, _ []byte) error {
				published.Store(true)
				return nil
			},
			uploadFileFn: func(_ context.Context, _ string, _ []byte, _ map[string]string) error {
				return nil
			},
		},
		name:                "high",
		healthy:             true,
		lastCheckTime:       time.Now(),
		fileTransferEnabled: true,
	}}

	channel := &mmModel.Channel{Id: "chan-id", Name: "ch", TeamId: "team-id"}
	team := &mmModel.Team{Id: "team-id", Name: "test-team"}
	user := &mmModel.User{Id: "user-id", Username: "alice"}

	api.On("GetChannel", "chan-id").Return(channel, nil)
	api.On("GetTeam", "team-id").Return(team, nil)
	api.On("GetUser", "user-id").Return(user, nil)

	fi := &mmModel.FileInfo{Id: "file-1", Name: "doc.pdf", Size: 1024}
	api.On("GetFileInfo", "file-1").Return(fi, nil)
	api.On("GetFile", "file-1").Return([]byte("file-data"), nil)
	maxSize := int64(100 * 1024 * 1024)
	api.On("GetConfig").Return(&mmModel.Config{
		FileSettings: mmModel.FileSettings{MaxFileSize: &maxSize},
	}).Maybe()

	post := &mmModel.Post{
		Id:        "p1",
		ChannelId: "chan-id",
		UserId:    "user-id",
		Message:   "hello with file",
		FileIds:   mmModel.StringArray{"file-1"},
	}
	p.MessageHasBeenPosted(nil, post)
	p.wg.Wait()

	assert.True(t, published.Load())
}

func TestMessageHasBeenUpdated_SystemMessageSkipped(t *testing.T) {
	api := &plugintest.API{}
	hooksLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)

	post := &mmModel.Post{Id: "p1", Type: mmModel.PostTypeJoinChannel, UserId: "user-id"}
	p.MessageHasBeenUpdated(nil, post, post)
	api.AssertNotCalled(t, "GetChannel", mock.Anything)
	api.AssertNotCalled(t, "GetUser", mock.Anything)
}

func TestMessageHasBeenUpdated_GetUserFails(t *testing.T) {
	api := &plugintest.API{}
	hooksLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)

	channel := &mmModel.Channel{Id: "chan-id", Name: "ch", TeamId: "team-id"}
	team := &mmModel.Team{Id: "team-id", Name: "test-team"}
	api.On("GetChannel", "chan-id").Return(channel, nil)
	api.On("GetTeam", "team-id").Return(team, nil)
	api.On("GetUser", "user-id").Return(nil, &mmModel.AppError{Message: "user not found"})

	post := &mmModel.Post{Id: "p1", ChannelId: "chan-id", UserId: "user-id", Message: "edited"}
	p.MessageHasBeenUpdated(nil, post, post)
	// LogError should have been called for the user lookup failure.
}

func TestMessageHasBeenDeleted_BotUser(t *testing.T) {
	api := &plugintest.API{}
	hooksLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)

	post := &mmModel.Post{Id: "p1", UserId: p.botUserID, ChannelId: "chan-id"}
	p.MessageHasBeenDeleted(nil, post)
	// Bot user posts should be filtered early, no KV interaction.
	api.AssertNotCalled(t, "GetChannel", mock.Anything)
}

func TestReactionHasBeenAdded_SystemPost(t *testing.T) {
	api := &plugintest.API{}
	hooksLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)

	systemPost := &mmModel.Post{Id: "post-id", Type: mmModel.PostTypeJoinChannel, UserId: "user-id"}
	api.On("GetPost", "post-id").Return(systemPost, nil)

	reaction := &mmModel.Reaction{PostId: "post-id", UserId: "reactor-id", EmojiName: "thumbsup"}
	p.ReactionHasBeenAdded(nil, reaction)
	// System post reactions should be skipped.
	api.AssertNotCalled(t, "GetUser", mock.Anything)
}

func TestReactionHasBeenAdded_CrossguardSyncUser(t *testing.T) {
	api := &plugintest.API{}
	hooksLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)

	normalPost := &mmModel.Post{Id: "post-id", UserId: "user-id", ChannelId: "chan-id"}
	syncUser := &mmModel.User{Id: "reactor-id", Username: "alice.high", Position: "crossguard-sync"}

	api.On("GetPost", "post-id").Return(normalPost, nil)
	api.On("GetUser", "reactor-id").Return(syncUser, nil)

	reaction := &mmModel.Reaction{PostId: "post-id", UserId: "reactor-id", EmojiName: "thumbsup"}
	p.ReactionHasBeenAdded(nil, reaction)
	// Sync user reactions should be filtered, no channel lookup.
	api.AssertNotCalled(t, "GetChannel", mock.Anything)
}

func TestReactionHasBeenAdded_GetPostFails(t *testing.T) {
	api := &plugintest.API{}
	hooksLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)

	api.On("GetPost", "post-id").Return(nil, &mmModel.AppError{Message: "not found"})

	reaction := &mmModel.Reaction{PostId: "post-id", UserId: "reactor-id", EmojiName: "thumbsup"}
	p.ReactionHasBeenAdded(nil, reaction)
	// GetUser should not be called when GetPost fails.
	api.AssertNotCalled(t, "GetUser", mock.Anything)
}

func TestRelayToOutbound_SemaphoreFull(t *testing.T) {
	api := &plugintest.API{}
	hooksLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)

	// Use a capacity-1 semaphore and fill it.
	p.relaySem = make(chan struct{}, 1)
	p.relaySem <- struct{}{}

	env := &model.Envelope{Type: model.MessageTypePost}
	conns := []store.TeamConnection{{Direction: "outbound", Connection: "high"}}
	p.relayToOutbound(env, conns, "test-semaphore-full")

	// Drain so test cleanup does not hang.
	<-p.relaySem
}
