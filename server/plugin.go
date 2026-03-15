package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	pluginapi "github.com/mattermost/mattermost/server/public/pluginapi"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

// Plugin implements the interface expected by the Mattermost server to communicate
// between the server and plugin processes.
type Plugin struct {
	plugin.MattermostPlugin

	client            *pluginapi.Client
	router            *mux.Router
	botUserID         string
	kvstore           store.KVStore
	configuration     *configuration
	configurationLock sync.RWMutex
}

func (p *Plugin) OnActivate() error {
	p.client = pluginapi.NewClient(p.API, p.Driver)

	botUserID, err := p.client.Bot.EnsureBot(&model.Bot{
		Username:    "crossguard",
		DisplayName: "Cross Guard",
		Description: "Cross Guard bot for cross-domain message relay.",
	})
	if err != nil {
		return fmt.Errorf("failed to ensure crossguard bot: %w", err)
	}
	p.botUserID = botUserID

	bundlePath, err := p.API.GetBundlePath()
	if err != nil {
		return fmt.Errorf("failed to get bundle path: %w", err)
	}

	profileImage, err := os.ReadFile(filepath.Join(bundlePath, "assets", "crossguard.png"))
	if err != nil {
		return fmt.Errorf("failed to read bot profile image: %w", err)
	}

	if appErr := p.API.SetProfileImage(botUserID, profileImage); appErr != nil {
		return fmt.Errorf("failed to set bot profile image: %w", appErr)
	}

	inner := store.NewKVStore(p.client, manifest.Id)
	p.kvstore = store.NewCachingKVStore(inner, p.API)

	if err := p.registerCommand(); err != nil {
		return err
	}

	p.initAPI()

	return nil
}

func (p *Plugin) OnDeactivate() error {
	return nil
}

func (p *Plugin) OnPluginClusterEvent(_ context.Context, ev model.PluginClusterEvent) {
	if caching, ok := p.kvstore.(*store.CachingKVStore); ok {
		caching.HandleClusterEvent(ev)
	}
}
