package model

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
)

// Format represents the wire format for message serialization.
type Format string

const (
	FormatJSON Format = "json"
	FormatXML  Format = "xml"
)

// Envelope is the top-level message wrapper for all formats.
// Exactly one payload field is non-nil per message.
type Envelope struct {
	XMLName         xml.Name         `json:"-"                          xml:"urn:mattermost-crossguard CrossguardMessage"`
	Type            string           `json:"type"                       xml:"Type"`
	Timestamp       string           `json:"timestamp"                  xml:"Timestamp"`
	PostMessage     *PostMessage     `json:"post_message,omitempty"     xml:"PostMessage,omitempty"`
	DeleteMessage   *DeleteMessage   `json:"delete_message,omitempty"   xml:"DeleteMessage,omitempty"`
	ReactionMessage *ReactionMessage `json:"reaction_message,omitempty" xml:"ReactionMessage,omitempty"`
	TestMessage     *TestMessage     `json:"test_message,omitempty"     xml:"TestMessage,omitempty"`
}

// Marshal serializes an Envelope to bytes in the given format.
func Marshal(env *Envelope, format Format) ([]byte, error) {
	switch format {
	case FormatXML:
		data, err := xml.Marshal(env)
		if err != nil {
			return nil, err
		}
		return append([]byte(xml.Header), data...), nil
	case FormatJSON:
		return json.Marshal(env)
	default:
		return nil, fmt.Errorf("unsupported format: %q", format)
	}
}

// Unmarshal deserializes bytes into an Envelope using the given format.
func Unmarshal(data []byte, format Format) (*Envelope, error) {
	var env Envelope
	switch format {
	case FormatXML:
		if err := xml.Unmarshal(data, &env); err != nil {
			return nil, err
		}
	case FormatJSON:
		if err := json.Unmarshal(data, &env); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported format: %q", format)
	}
	return &env, nil
}

// DetectFormat checks the first non-whitespace byte to determine format.
// Returns FormatXML if the first non-whitespace byte is '<', FormatJSON otherwise.
// A leading UTF-8 BOM (0xEF 0xBB 0xBF) is stripped before detection.
func DetectFormat(data []byte) Format {
	// Strip UTF-8 BOM if present.
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}
	for _, b := range data {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		case '<':
			return FormatXML
		default:
			return FormatJSON
		}
	}
	return FormatJSON
}
