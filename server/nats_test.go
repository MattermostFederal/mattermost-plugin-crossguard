package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

func TestIsOutboundLinked(t *testing.T) {
	tests := []struct {
		name         string
		outboundName string
		connNames    []store.TeamConnection
		expected     bool
	}{
		{
			name:         "linked outbound connection",
			outboundName: "high",
			connNames: []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
				{Direction: "inbound", Connection: "high"},
			},
			expected: true,
		},
		{
			name:         "not linked outbound connection",
			outboundName: "other",
			connNames: []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
				{Direction: "inbound", Connection: "high"},
			},
			expected: false,
		},
		{
			name:         "inbound name does not match outbound check",
			outboundName: "high",
			connNames: []store.TeamConnection{
				{Direction: "inbound", Connection: "high"},
			},
			expected: false,
		},
		{
			name:         "empty connection list",
			outboundName: "high",
			connNames:    nil,
			expected:     false,
		},
		{
			name:         "partial name match does not count",
			outboundName: "hig",
			connNames: []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
			},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isOutboundLinked(tc.outboundName, tc.connNames)
			assert.Equal(t, tc.expected, result)
		})
	}
}
