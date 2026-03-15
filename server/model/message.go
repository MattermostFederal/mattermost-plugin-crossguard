package model

import (
	"encoding/json"
	"fmt"
	"time"
)

// Message is the standard envelope format for all messages sent over NATS.
type Message struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	JSON      string `json:"json"`
}

// NewMessage creates a Message envelope by serializing the payload into the JSON field.
func NewMessage(msgType string, payload any) (*Message, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	return &Message{
		Type:      msgType,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		JSON:      string(data),
	}, nil
}

// Decode deserializes the JSON field into dest.
func (m *Message) Decode(dest any) error {
	if err := json.Unmarshal([]byte(m.JSON), dest); err != nil {
		return fmt.Errorf("failed to decode message payload: %w", err)
	}
	return nil
}

// Marshal serializes the full envelope to bytes.
func Marshal(m *Message) ([]byte, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message envelope: %w", err)
	}
	return data, nil
}

// UnmarshalMessage deserializes bytes into a Message envelope.
func UnmarshalMessage(data []byte) (*Message, error) {
	var m Message
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message envelope: %w", err)
	}
	return &m, nil
}
