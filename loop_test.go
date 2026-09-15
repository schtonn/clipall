package main

import (
	"testing"
	"time"
)

func event(contentID uint64, source string, timestamp int64) EventID {
	return EventID{ContentID: contentID, Source: source, Timestamp: timestamp}
}

func TestRingBufferEmpty(t *testing.T) {
	var r RingBuffer
	if r.Contains(event(1, "node-a", 1)) {
		t.Fatal("Contains returned true on empty buffer")
	}
}

func TestRingBufferAddContainsExactEvent(t *testing.T) {
	var r RingBuffer
	id := event(42, "node-a", 100)
	r.Add(id)
	if !r.Contains(id) {
		t.Fatal("Contains returned false for added event")
	}
}

func TestRingBufferSameContentDifferentSourceOrTimestamp(t *testing.T) {
	var r RingBuffer
	r.Add(event(42, "node-a", 100))

	if r.Contains(event(42, "node-b", 100)) {
		t.Fatal("same content from a different source is a different event")
	}
	if r.Contains(event(42, "node-a", 101)) {
		t.Fatal("same content with a different timestamp is a different event")
	}
}

func TestRingBufferWrapAround(t *testing.T) {
	var r RingBuffer
	for i := uint64(1); i <= ringSize+10; i++ {
		r.Add(event(i, "node-a", int64(i)))
	}
	for i := uint64(1); i <= 10; i++ {
		if r.Contains(event(i, "node-a", int64(i))) {
			t.Fatalf("Contains returned true for evicted event %d", i)
		}
	}
	for i := uint64(11); i <= ringSize+10; i++ {
		if !r.Contains(event(i, "node-a", int64(i))) {
			t.Fatalf("Contains returned false for retained event %d", i)
		}
	}
}

func TestEchoBufferOnlyConsumesMatchingContentAndType(t *testing.T) {
	var echoes EchoBuffer
	now := time.Now()
	id := event(42, "remote", 100)
	echoes.Add(TypeText, 42, id, now)

	if _, ok := echoes.Consume(TypeText, 99, now); ok {
		t.Fatal("unrelated clipboard content was suppressed")
	}
	if _, ok := echoes.Consume(TypeImage, 42, now); ok {
		t.Fatal("same content hash with a different type was suppressed")
	}
	got, ok := echoes.Consume(TypeText, 42, now)
	if !ok || got != id {
		t.Fatalf("matching echo = (%+v, %v), want (%+v, true)", got, ok, id)
	}
	if _, ok := echoes.Consume(TypeText, 42, now); ok {
		t.Fatal("echo marker was not consumed")
	}
}

func TestEchoBufferExpires(t *testing.T) {
	var echoes EchoBuffer
	now := time.Now()
	echoes.Add(TypeText, 42, event(42, "remote", 100), now)
	if _, ok := echoes.Consume(TypeText, 42, now.Add(echoTTL)); ok {
		t.Fatal("expired echo marker suppressed a later clipboard event")
	}
}
