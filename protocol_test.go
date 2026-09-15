package main

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func mustEncode(t *testing.T, msg Message) []byte {
	t.Helper()
	encoded, err := Encode(msg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	return encoded
}

func TestEncodeDecodeText(t *testing.T) {
	original := Message{
		Type:      TypeText,
		ContentID: 42,
		Source:    "macbook",
		Timestamp: 1720000000123456789,
		Payload:   []byte("hello clipboard"),
	}

	encoded := mustEncode(t, original)
	decoded, err := Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if decoded.Version != ProtocolVersion {
		t.Errorf("Version = %d, want %d", decoded.Version, ProtocolVersion)
	}
	if decoded.Type != TypeText {
		t.Errorf("Type = %d, want %d", decoded.Type, TypeText)
	}
	if decoded.ContentID != 42 {
		t.Errorf("ContentID = %d, want 42", decoded.ContentID)
	}
	if decoded.Source != original.Source {
		t.Errorf("Source = %q, want %q", decoded.Source, original.Source)
	}
	if decoded.Timestamp != original.Timestamp {
		t.Errorf("Timestamp = %d, want %d", decoded.Timestamp, original.Timestamp)
	}
	if !bytes.Equal(decoded.Payload, original.Payload) {
		t.Errorf("Payload = %q, want %q", decoded.Payload, original.Payload)
	}
}

func TestEncodeDecodeImage(t *testing.T) {
	payload := make([]byte, 64*1024) // 64KB fake image
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	original := Message{
		Type:      TypeImage,
		ContentID: 99,
		Source:    "windows-pc",
		Timestamp: 1720000000123456790,
		Payload:   payload,
	}

	encoded := mustEncode(t, original)
	decoded, err := Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if decoded.Type != TypeImage {
		t.Errorf("Type = %d, want %d", decoded.Type, TypeImage)
	}
	if !bytes.Equal(decoded.Payload, payload) {
		t.Errorf("Payload mismatch: got %d bytes, want %d bytes", len(decoded.Payload), len(payload))
	}
}

func TestEncodeDecodeEmptyPayload(t *testing.T) {
	original := Message{
		Type:      TypePing,
		ContentID: 1,
		Payload:   []byte{},
	}

	encoded := mustEncode(t, original)
	decoded, err := Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if decoded.Type != TypePing {
		t.Errorf("Type = %d, want %d", decoded.Type, TypePing)
	}
	if len(decoded.Payload) != 0 {
		t.Errorf("Payload length = %d, want 0", len(decoded.Payload))
	}
}

func TestDecodeVersionMismatch(t *testing.T) {
	msg := Message{
		Type:      TypeText,
		ContentID: 1,
		Payload:   []byte("test"),
	}

	encoded := mustEncode(t, msg)
	encoded[0] = 99 // corrupt version byte

	_, err := Decode(bytes.NewReader(encoded))
	if err == nil {
		t.Fatal("expected error for version mismatch, got nil")
	}
}

func TestDecodePayloadTooLarge(t *testing.T) {
	msg := Message{
		Type:      TypeText,
		ContentID: 1,
		Payload:   []byte("small"),
	}

	encoded := mustEncode(t, msg)

	// Overwrite PayloadLen with a value exceeding MaxPayloadSize in the v2 header.
	// MaxPayloadSize is 10*1024*1024 = 10485760, so use 10485761.
	encoded[20] = 0x00
	encoded[21] = 0xA0
	encoded[22] = 0x00
	encoded[23] = 0x01

	_, err := Decode(bytes.NewReader(encoded))
	if err == nil {
		t.Fatal("expected error for payload too large, got nil")
	}
}

func TestDecodeSequentialMessages(t *testing.T) {
	textMsg := Message{
		Type:      TypeText,
		ContentID: 1,
		Payload:   []byte("hello clipboard"),
	}
	imgPayload := make([]byte, 256)
	for i := range imgPayload {
		imgPayload[i] = byte(i % 256)
	}
	imgMsg := Message{
		Type:      TypeImage,
		ContentID: 2,
		Payload:   imgPayload,
	}

	// Concatenate both encoded messages into a single buffer, as a TCP
	// connection would deliver them.
	var buf bytes.Buffer
	buf.Write(mustEncode(t, textMsg))
	buf.Write(mustEncode(t, imgMsg))

	// Decode the first message.
	got1, err := Decode(&buf)
	if err != nil {
		t.Fatalf("Decode first message: %v", err)
	}
	if got1.Type != TypeText {
		t.Errorf("first message Type = 0x%02x, want 0x%02x", got1.Type, TypeText)
	}
	if got1.ContentID != 1 {
		t.Errorf("first message ContentID = %d, want 1", got1.ContentID)
	}
	if !bytes.Equal(got1.Payload, textMsg.Payload) {
		t.Errorf("first message Payload = %q, want %q", got1.Payload, textMsg.Payload)
	}

	// Decode the second message from the same buffer.
	got2, err := Decode(&buf)
	if err != nil {
		t.Fatalf("Decode second message: %v", err)
	}
	if got2.Type != TypeImage {
		t.Errorf("second message Type = 0x%02x, want 0x%02x", got2.Type, TypeImage)
	}
	if got2.ContentID != 2 {
		t.Errorf("second message ContentID = %d, want 2", got2.ContentID)
	}
	if !bytes.Equal(got2.Payload, imgPayload) {
		t.Errorf("second message Payload mismatch: got %d bytes, want %d bytes",
			len(got2.Payload), len(imgPayload))
	}

	// Buffer should now be empty; a third Decode should fail.
	_, err = Decode(&buf)
	if err == nil {
		t.Fatal("expected error decoding from exhausted buffer, got nil")
	}
}

func TestEncodeDecodePreservesContentID(t *testing.T) {
	contentID := uint64(0xDEADBEEFCAFEBABE)

	original := Message{
		Type:      TypeText,
		ContentID: contentID,
		Payload:   []byte("id test"),
	}

	encoded := mustEncode(t, original)
	decoded, err := Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if decoded.ContentID != contentID {
		t.Errorf("ContentID = 0x%X, want 0x%X", decoded.ContentID, contentID)
	}
}

func TestEncodeRejectsOversizedPayload(t *testing.T) {
	_, err := Encode(Message{Type: TypeText, Payload: make([]byte, MaxPayloadSize+1)})
	if err == nil {
		t.Fatal("expected Encode to reject oversized payload")
	}
}

func TestEncodeRejectsOversizedSource(t *testing.T) {
	_, err := Encode(Message{Type: TypeText, Source: string(make([]byte, MaxSourceSize+1))})
	if err == nil {
		t.Fatal("expected Encode to reject oversized source")
	}
}

func TestDecodeLegacyV1Message(t *testing.T) {
	payload := []byte("from an older peer")
	encoded := make([]byte, legacyHeaderSize+len(payload))
	encoded[0] = LegacyProtocolVersion
	encoded[1] = byte(TypeText)
	binary.BigEndian.PutUint64(encoded[2:10], 42)
	binary.BigEndian.PutUint32(encoded[10:14], uint32(len(payload)))
	copy(encoded[14:], payload)

	decoded, err := Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("Decode legacy message: %v", err)
	}
	if decoded.Version != LegacyProtocolVersion || decoded.ContentID != 42 {
		t.Fatalf("decoded legacy metadata = %+v", decoded)
	}
	if decoded.Source != "" || decoded.Timestamp != 0 {
		t.Fatalf("legacy message unexpectedly has v2 identity fields: %+v", decoded)
	}
	if !bytes.Equal(decoded.Payload, payload) {
		t.Fatalf("legacy payload = %q, want %q", decoded.Payload, payload)
	}
}
