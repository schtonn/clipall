package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cespare/xxhash/v2"
)

func TestNewNodeInitialization(t *testing.T) {
	n := NewNode(9876, []string{"host-a:9876", "host-b:9876"}, "", 0)

	if n.listenPort != 9876 {
		t.Errorf("listenPort = %d, want 9876", n.listenPort)
	}
	if len(n.peers) != 2 {
		t.Fatalf("len(peers) = %d, want 2", len(n.peers))
	}
	if n.peers[0].addr != "host-a:9876" {
		t.Errorf("peers[0].addr = %q, want %q", n.peers[0].addr, "host-a:9876")
	}
	if n.peers[1].addr != "host-b:9876" {
		t.Errorf("peers[1].addr = %q, want %q", n.peers[1].addr, "host-b:9876")
	}
	if n.incoming == nil {
		t.Fatal("incoming channel is nil")
	}
}

func TestNewNodeNoPeers(t *testing.T) {
	n := NewNode(5555, nil, "", 0)

	if n.listenPort != 5555 {
		t.Errorf("listenPort = %d, want 5555", n.listenPort)
	}
	if len(n.peers) != 0 {
		t.Fatalf("len(peers) = %d, want 0", len(n.peers))
	}
}

func TestIncomingTextRingBufferDedup(t *testing.T) {
	n := NewNode(9876, nil, "", 0)

	payload := []byte("hello clipboard")
	id := xxhash.Sum64(payload)

	msg := Message{
		Type:      TypeText,
		ContentID: id,
		Source:    "node-a",
		Timestamp: 100,
		Payload:   payload,
	}

	// First time: not in ring.
	if n.ring.Contains(msg.EventID()) {
		t.Fatal("ring should not contain ID before Add")
	}

	// Simulate what the incoming handler does: add to ring.
	n.ring.Add(msg.EventID())

	// Second time: ring should reject the duplicate.
	if !n.ring.Contains(msg.EventID()) {
		t.Fatal("ring should contain ID after Add")
	}
}

func TestIncomingImageRingBufferDedup(t *testing.T) {
	n := NewNode(9876, nil, "", 0)

	payload := make([]byte, 1024)
	for i := range payload {
		payload[i] = byte(i % 256)
	}
	id := xxhash.Sum64(payload)

	msg := Message{
		Type:      TypeImage,
		ContentID: id,
		Source:    "node-a",
		Timestamp: 100,
		Payload:   payload,
	}

	if n.ring.Contains(msg.EventID()) {
		t.Fatal("ring should not contain image ID before Add")
	}

	n.ring.Add(msg.EventID())

	if !n.ring.Contains(msg.EventID()) {
		t.Fatal("ring should contain image ID after Add")
	}
}

func TestIncomingMixedTypeDedup(t *testing.T) {
	n := NewNode(9876, nil, "", 0)

	textPayload := []byte("some text")
	textID := xxhash.Sum64(textPayload)

	imgPayload := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A} // PNG header
	imgID := xxhash.Sum64(imgPayload)

	// Simulate receiving a text message, then an image message.
	textEvent := EventID{ContentID: textID, Source: "node-a", Timestamp: 100}
	imgEvent := EventID{ContentID: imgID, Source: "node-a", Timestamp: 101}
	n.ring.Add(textEvent)
	n.ring.Add(imgEvent)

	// Both should now be detected as duplicates.
	if !n.ring.Contains(textEvent) {
		t.Fatal("ring should contain text ID")
	}
	if !n.ring.Contains(imgEvent) {
		t.Fatal("ring should contain image ID")
	}

	// A new, different payload should not be blocked.
	otherPayload := []byte("different content")
	otherID := xxhash.Sum64(otherPayload)
	if n.ring.Contains(EventID{ContentID: otherID, Source: "node-a", Timestamp: 102}) {
		t.Fatal("ring should not contain ID for content never added")
	}
}

func TestIncomingChannelCapacity(t *testing.T) {
	n := NewNode(9876, nil, "", 0)

	// The incoming channel has a buffer of 32. Fill it without blocking.
	for i := 0; i < 32; i++ {
		msg := Message{
			Type:      TypeText,
			ContentID: uint64(i),
			Payload:   []byte("test"),
		}
		select {
		case n.incoming <- msg:
		default:
			t.Fatalf("incoming channel blocked at message %d, expected buffer of 32", i)
		}
	}

	// The 33rd send should not block (we verify by using select/default).
	select {
	case n.incoming <- Message{Type: TypeText, ContentID: 999, Payload: []byte("overflow")}:
		t.Fatal("incoming channel accepted message beyond capacity 32 without a reader")
	default:
		// Expected: channel is full.
	}
}

func TestIncomingDrainBothTypes(t *testing.T) {
	n := NewNode(9876, nil, "", 0)

	textMsg := Message{
		Type:      TypeText,
		ContentID: 1,
		Payload:   []byte("hello"),
	}
	imgMsg := Message{
		Type:      TypeImage,
		ContentID: 2,
		Payload:   []byte{0xFF, 0xD8, 0xFF, 0xE0}, // JPEG SOI marker
	}

	n.incoming <- textMsg
	n.incoming <- imgMsg

	// Drain and verify both types arrive in order.
	got1 := <-n.incoming
	if got1.Type != TypeText {
		t.Errorf("first message type = 0x%02x, want 0x%02x (TypeText)", got1.Type, TypeText)
	}
	if got1.ContentID != 1 {
		t.Errorf("first message ContentID = %d, want 1", got1.ContentID)
	}

	got2 := <-n.incoming
	if got2.Type != TypeImage {
		t.Errorf("second message type = 0x%02x, want 0x%02x (TypeImage)", got2.Type, TypeImage)
	}
	if got2.ContentID != 2 {
		t.Errorf("second message ContentID = %d, want 2", got2.ContentID)
	}
}

func TestNewMessageTreatsRepeatedContentAsDistinctEvents(t *testing.T) {
	n := NewNode(9876, nil, "", 0)
	n.source = "node-a"

	data := []byte("clipboard text")
	first := n.newMessage(TypeText, data)
	second := n.newMessage(TypeText, data)

	if first.ContentID != second.ContentID {
		t.Fatal("identical clipboard content should have the same content hash")
	}
	if first.Source != "node-a" || second.Source != "node-a" {
		t.Fatal("local source was not attached to clipboard events")
	}
	if second.Timestamp <= first.Timestamp {
		t.Fatalf("timestamps are not strictly increasing: first=%d second=%d", first.Timestamp, second.Timestamp)
	}
	if first.EventID() == second.EventID() {
		t.Fatal("repeated copies must have distinct event IDs")
	}
}

func TestSaveImageTimestampedFilename(t *testing.T) {
	dir := t.TempDir()
	n := NewNode(9876, nil, dir, 0)

	data := []byte("fake-png-data")
	path, err := n.saveImage(data)
	if err != nil {
		t.Fatalf("saveImage: %v", err)
	}

	// Should create a timestamped file.
	if filepath.Dir(path) != dir {
		t.Errorf("image saved to wrong directory: %s", path)
	}
	base := filepath.Base(path)
	if len(base) < 19 { // "20060102-150405.000.png" = 23 chars
		t.Errorf("filename too short, expected timestamp format: %s", base)
	}

	// latest.png should also exist with same content.
	latestPath := filepath.Join(dir, "latest.png")
	latestData, err := os.ReadFile(latestPath)
	if err != nil {
		t.Fatalf("latest.png not created: %v", err)
	}
	if string(latestData) != string(data) {
		t.Error("latest.png content does not match")
	}
}

func TestSaveImageMultipleDoNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	n := NewNode(9876, nil, dir, 0)

	n.saveImage([]byte("image-1"))
	time.Sleep(2 * time.Millisecond) // ensure different timestamp
	n.saveImage([]byte("image-2"))

	entries, _ := os.ReadDir(dir)
	// Should have 2 timestamped files + latest.png = 3 files.
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			count++
		}
	}
	if count != 3 {
		t.Errorf("expected 3 files (2 timestamped + latest.png), got %d", count)
	}
}

func TestPruneImagesRemovesOldest(t *testing.T) {
	dir := t.TempDir()
	n := NewNode(9876, nil, dir, 1) // 1 MB limit

	// Write files that total > 1MB.
	bigData := make([]byte, 400*1024) // 400KB each
	for i := range bigData {
		bigData[i] = byte(i)
	}

	n.saveImage(bigData)
	time.Sleep(10 * time.Millisecond)
	n.saveImage(bigData)
	time.Sleep(10 * time.Millisecond)
	n.saveImage(bigData) // 3 * 400KB = 1.2MB > 1MB, should trigger prune

	entries, _ := os.ReadDir(dir)
	var pngCount int
	for _, e := range entries {
		if !e.IsDir() && e.Name() != "latest.png" && filepath.Ext(e.Name()) == ".png" {
			pngCount++
		}
	}
	// At least one old file should have been pruned.
	if pngCount >= 3 {
		t.Errorf("expected pruning to remove old files, but found %d timestamped files", pngCount)
	}
}

func TestPruneImagesKeepsLatestPng(t *testing.T) {
	dir := t.TempDir()
	n := NewNode(9876, nil, dir, 1) // 1 MB limit

	bigData := make([]byte, 600*1024) // 600KB
	n.saveImage(bigData)
	time.Sleep(10 * time.Millisecond)
	n.saveImage(bigData) // 2 * 600KB = 1.2MB > 1MB

	// latest.png should still exist.
	if _, err := os.Stat(filepath.Join(dir, "latest.png")); err != nil {
		t.Error("latest.png should not be pruned")
	}
}

func TestPruneImagesSkipsNonPngFiles(t *testing.T) {
	dir := t.TempDir()
	n := NewNode(9876, nil, dir, 1) // 1 MB limit

	// Create a non-png file in the directory.
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("keep me"), 0644)

	bigData := make([]byte, 600*1024)
	n.saveImage(bigData)
	time.Sleep(10 * time.Millisecond)
	n.saveImage(bigData)

	// notes.txt should still exist.
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Error("non-png file should not be pruned")
	}
}

func TestPruneImagesUnlimited(t *testing.T) {
	dir := t.TempDir()
	n := NewNode(9876, nil, dir, 0) // 0 = unlimited

	bigData := make([]byte, 500*1024)
	for i := 0; i < 5; i++ {
		n.saveImage(bigData)
		time.Sleep(2 * time.Millisecond)
	}

	entries, _ := os.ReadDir(dir)
	var pngCount int
	for _, e := range entries {
		if !e.IsDir() && e.Name() != "latest.png" && filepath.Ext(e.Name()) == ".png" {
			pngCount++
		}
	}
	if pngCount != 5 {
		t.Errorf("with unlimited size, expected 5 timestamped files, got %d", pngCount)
	}
}

func TestPeerSendQueueing(t *testing.T) {
	p := NewPeer("fake-host:9876")

	msg := Message{
		Type:      TypeImage,
		ContentID: 42,
		Payload:   []byte{0x89, 0x50, 0x4E, 0x47},
	}

	// Send should succeed (queued in the channel buffer of 16).
	p.Send(msg)

	select {
	case got := <-p.sendCh:
		if got.Type != TypeImage {
			t.Errorf("queued message type = 0x%02x, want 0x%02x", got.Type, TypeImage)
		}
		if got.ContentID != 42 {
			t.Errorf("queued message ContentID = %d, want 42", got.ContentID)
		}
	default:
		t.Fatal("expected message in peer send channel")
	}
}

func TestPeerSendDropsWhenFull(t *testing.T) {
	p := NewPeer("fake-host:9876")

	// Fill the send channel (capacity 16).
	for i := 0; i < 16; i++ {
		p.Send(Message{Type: TypeText, ContentID: uint64(i), Payload: []byte("x")})
	}

	// The 17th message should be silently dropped, not block.
	p.Send(Message{Type: TypeText, ContentID: 999, Payload: []byte("dropped")})

	// Verify all 16 buffered messages are the originals.
	for i := 0; i < 16; i++ {
		got := <-p.sendCh
		if got.ContentID != uint64(i) {
			t.Errorf("message %d: ContentID = %d, want %d", i, got.ContentID, i)
		}
	}
}
