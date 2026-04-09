package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTeamConnection_Matches_SameDirectionAndConnection(t *testing.T) {
	a := TeamConnection{Direction: "outbound", Connection: "nats-relay", RemoteTeamName: "Team A"}
	b := TeamConnection{Direction: "outbound", Connection: "nats-relay", RemoteTeamName: "Team A"}
	assert.True(t, a.Matches(b))
}

func TestTeamConnection_Matches_DifferentDirection(t *testing.T) {
	a := TeamConnection{Direction: "outbound", Connection: "nats-relay"}
	b := TeamConnection{Direction: "inbound", Connection: "nats-relay"}
	assert.False(t, a.Matches(b))
}

func TestTeamConnection_Matches_DifferentConnection(t *testing.T) {
	a := TeamConnection{Direction: "outbound", Connection: "nats-relay"}
	b := TeamConnection{Direction: "outbound", Connection: "azure-queue"}
	assert.False(t, a.Matches(b))
}

func TestTeamConnection_Matches_IgnoresRemoteTeamName(t *testing.T) {
	a := TeamConnection{Direction: "inbound", Connection: "nats-relay", RemoteTeamName: "Team Alpha"}
	b := TeamConnection{Direction: "inbound", Connection: "nats-relay", RemoteTeamName: "Team Beta"}
	assert.True(t, a.Matches(b))
}

func TestTeamConnection_Matches_BothEmpty(t *testing.T) {
	a := TeamConnection{}
	b := TeamConnection{}
	assert.True(t, a.Matches(b))
}

func TestPromptStateConstants(t *testing.T) {
	assert.Equal(t, "pending", PromptStatePending)
	assert.Equal(t, "blocked", PromptStateBlocked)
}
