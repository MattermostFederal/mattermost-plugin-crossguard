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
			outboundName: "cgb",
			connNames:    []string{"outbound:cgb", "inbound:cgb"},
			expected:     true,
		},
		{
			name:         "not linked outbound connection",
			outboundName: "other",
			connNames:    []string{"outbound:cgb", "inbound:cgb"},
			expected:     false,
		},
		{
			name:         "inbound name does not match outbound check",
			outboundName: "cgb",
			connNames:    []string{"inbound:cgb"},
			expected:     false,
		},
		{
			name:         "empty connection list",
			outboundName: "cgb",
			connNames:    nil,
			expected:     false,
		},
		{
			name:         "partial name match does not count",
			outboundName: "cg",
			connNames:    []string{"outbound:cgb"},
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
