package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsOutboundLinked(t *testing.T) {
	tests := []struct {
		name         string
		outboundName string
		connNames    []string
		expected     bool
	}{
		{
			name:         "linked outbound connection",
			outboundName: "high",
			connNames:    []string{"outbound:high", "inbound:high"},
			expected:     true,
		},
		{
			name:         "not linked outbound connection",
			outboundName: "other",
			connNames:    []string{"outbound:high", "inbound:high"},
			expected:     false,
		},
		{
			name:         "inbound name does not match outbound check",
			outboundName: "high",
			connNames:    []string{"inbound:high"},
			expected:     false,
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
			connNames:    []string{"outbound:high"},
			expected:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isOutboundLinked(tc.outboundName, tc.connNames)
			assert.Equal(t, tc.expected, result)
		})
	}
}
