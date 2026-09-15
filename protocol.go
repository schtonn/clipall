package main

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	LegacyProtocolVersion = 1
	ProtocolVersion       = 2
)

type MessageType byte

const (
	TypeText  MessageType = 0x01
	TypeImage MessageType = 0x02
	TypePing  MessageType = 0x03
	TypePong  MessageType = 0x04
)

const MaxPayloadSize = 10 * 1024 * 1024 // 10MB
const MaxSourceSize = 1024

const (
	legacyHeaderSize = 14
	headerSize       = 24
)

type Message struct {
	Version   byte
	Type      MessageType
	ContentID uint64
	Source    string
	Timestamp int64
	Payload   []byte
}

func (m Message) EventID() EventID {
	return EventID{ContentID: m.ContentID, Source: m.Source, Timestamp: m.Timestamp}
}

// Encode serializes a Message to wire format.
// Always sets Version to ProtocolVersion regardless of the input value.
func Encode(msg Message) ([]byte, error) {
	source := []byte(msg.Source)
	if len(source) > MaxSourceSize {
		return nil, fmt.Errorf("source too large: %d bytes exceeds max %d", len(source), MaxSourceSize)
	}
	if len(msg.Payload) > MaxPayloadSize {
		return nil, fmt.Errorf("payload too large: %d bytes exceeds max %d", len(msg.Payload), MaxPayloadSize)
	}
	payloadLen := uint32(len(msg.Payload))
	buf := make([]byte, headerSize+len(source)+int(payloadLen))

	buf[0] = ProtocolVersion
	buf[1] = byte(msg.Type)
	binary.BigEndian.PutUint64(buf[2:10], msg.ContentID)
	binary.BigEndian.PutUint64(buf[10:18], uint64(msg.Timestamp))
	binary.BigEndian.PutUint16(buf[18:20], uint16(len(source)))
	binary.BigEndian.PutUint32(buf[20:24], payloadLen)
	copy(buf[24:], source)
	copy(buf[24+len(source):], msg.Payload)

	return buf, nil
}

// Decode reads a single Message from r.
// It also accepts legacy v1 messages during rolling upgrades. Returns an error
// for unsupported versions/types or oversized metadata and payloads.
func Decode(r io.Reader) (Message, error) {
	prefix := make([]byte, 2)
	if _, err := io.ReadFull(r, prefix); err != nil {
		return Message{}, fmt.Errorf("reading header: %w", err)
	}

	version := prefix[0]
	if version != LegacyProtocolVersion && version != ProtocolVersion {
		return Message{}, fmt.Errorf("version mismatch: got %d, want %d", version, ProtocolVersion)
	}

	msgType := MessageType(prefix[1])
	switch msgType {
	case TypeText, TypeImage, TypePing, TypePong:
	default:
		return Message{}, fmt.Errorf("unknown message type: 0x%02x", msgType)
	}

	remainingHeaderSize := legacyHeaderSize - len(prefix)
	if version == ProtocolVersion {
		remainingHeaderSize = headerSize - len(prefix)
	}
	header := make([]byte, remainingHeaderSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return Message{}, fmt.Errorf("reading header: %w", err)
	}

	contentID := binary.BigEndian.Uint64(header[0:8])
	var timestamp int64
	var sourceLen uint16
	var payloadLen uint32
	if version == LegacyProtocolVersion {
		payloadLen = binary.BigEndian.Uint32(header[8:12])
	} else {
		timestamp = int64(binary.BigEndian.Uint64(header[8:16]))
		sourceLen = binary.BigEndian.Uint16(header[16:18])
		payloadLen = binary.BigEndian.Uint32(header[18:22])
	}

	if payloadLen > MaxPayloadSize {
		return Message{}, fmt.Errorf("payload too large: %d bytes exceeds max %d", payloadLen, MaxPayloadSize)
	}
	if sourceLen > MaxSourceSize {
		return Message{}, fmt.Errorf("source too large: %d bytes exceeds max %d", sourceLen, MaxSourceSize)
	}

	source := make([]byte, sourceLen)
	if sourceLen > 0 {
		if _, err := io.ReadFull(r, source); err != nil {
			return Message{}, fmt.Errorf("reading source: %w", err)
		}
	}

	payload := make([]byte, payloadLen)
	if payloadLen > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return Message{}, fmt.Errorf("reading payload: %w", err)
		}
	}

	return Message{
		Version:   version,
		Type:      msgType,
		ContentID: contentID,
		Source:    string(source),
		Timestamp: timestamp,
		Payload:   payload,
	}, nil
}
