package main

import (
	"sync"

	"github.com/mattermost/mattermost/server/public/plugin"
	pluginapi "github.com/mattermost/mattermost/server/public/pluginapi"
)

// Plugin implements the interface expected by the Mattermost server to communicate
// between the server and plugin processes.
type Plugin struct {
	plugin.MattermostPlugin

	client            *pluginapi.Client
	configuration     *configuration
	configurationLock sync.RWMutex
}

func (p *Plugin) OnActivate() error {
	p.client = pluginapi.NewClient(p.API, p.Driver)

	if err := p.registerCommand(); err != nil {
		return err
	}

	return nil
}

func (p *Plugin) OnDeactivate() error {
	return nil
}
