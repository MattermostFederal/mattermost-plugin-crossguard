package model

// MessageTypeTest is the envelope type for connectivity test messages.
const MessageTypeTest = "crossguard_test"

// TestMessage represents a test message sent through NATS to verify connectivity.
type TestMessage struct {
	ID string `json:"id" xml:"ID"`
}
