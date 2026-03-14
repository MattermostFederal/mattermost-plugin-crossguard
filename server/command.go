package main

import (
	"fmt"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
)

const (
	commandTrigger = "crossguard"
)

func (p *Plugin) registerCommand() error {
	return p.API.RegisterCommand(&model.Command{
		Trigger:          commandTrigger,
		AutoComplete:     true,
		AutoCompleteDesc: "Cross Guard commands",
		AutoCompleteHint: "[init]",
		AutocompleteData: getAutocompleteData(),
	})
}

func getAutocompleteData() *model.AutocompleteData {
	cmd := model.NewAutocompleteData(commandTrigger, "[command]", "Cross Guard commands")

	init := model.NewAutocompleteData("init", "", "Initialize Cross Guard for this team")
	cmd.AddCommand(init)

	return cmd
}

func (p *Plugin) ExecuteCommand(_ *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	parts := strings.Fields(args.Command)
	if len(parts) < 2 {
		return respondEphemeral("Usage: /%s [init]", commandTrigger), nil
	}

	subcommand := parts[1]
	switch subcommand {
	case "init":
		return p.executeInit(args), nil
	default:
		return respondEphemeral("Unknown subcommand: %s. Usage: /%s [init]", subcommand, commandTrigger), nil
	}
}

func (p *Plugin) executeInit(args *model.CommandArgs) *model.CommandResponse {
	_ = args
	return respondEphemeral("Cross Guard initialized for this team.")
}

func respondEphemeral(format string, a ...any) *model.CommandResponse {
	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         fmt.Sprintf(format, a...),
	}
}
