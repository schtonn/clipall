package main

import "time"

const ringSize = 32

// EventID uniquely identifies one clipboard event. ContentID alone is not
// sufficient: copying the same value again is a new event and must be synced.
type EventID struct {
	ContentID uint64
	Source    string
	Timestamp int64
}

// RingBuffer is a fixed-size ring buffer of clipboard event IDs used to
// prevent network loops. The zero value is ready to use.
type RingBuffer struct {
	buf   [ringSize]EventID
	pos   int
	count int
}

// Add inserts an event ID into the ring buffer, advancing the write position.
func (r *RingBuffer) Add(id EventID) {
	r.buf[r.pos] = id
	r.pos = (r.pos + 1) % ringSize
	if r.count < ringSize {
		r.count++
	}
}

// Contains reports whether the complete event ID is present in the ring.
func (r *RingBuffer) Contains(id EventID) bool {
	n := r.count
	if n > ringSize {
		n = ringSize
	}
	for i := 0; i < n; i++ {
		if r.buf[i] == id {
			return true
		}
	}
	return false
}

const echoTTL = 5 * time.Second

type expectedEcho struct {
	type_     MessageType
	contentID uint64
	event     EventID
	expires   time.Time
}

// EchoBuffer tracks only clipboard notifications expected as a direct result
// of a remote write. Unlike a global cooldown, it never suppresses unrelated
// clipboard changes.
type EchoBuffer struct {
	entries []expectedEcho
}

func (e *EchoBuffer) Add(typ MessageType, contentID uint64, event EventID, now time.Time) {
	entry := expectedEcho{
		type_:     typ,
		contentID: contentID,
		event:     event,
		expires:   now.Add(echoTTL),
	}
	if len(e.entries) == ringSize {
		copy(e.entries, e.entries[1:])
		e.entries[len(e.entries)-1] = entry
		return
	}
	e.entries = append(e.entries, entry)
}

// Consume removes and returns the matching expected echo. Expired entries are
// discarded so a missing OS notification cannot suppress a later user copy.
func (e *EchoBuffer) Consume(typ MessageType, contentID uint64, now time.Time) (EventID, bool) {
	kept := e.entries[:0]
	var matched EventID
	found := false
	for _, entry := range e.entries {
		if !entry.expires.After(now) {
			continue
		}
		if !found && entry.type_ == typ && entry.contentID == contentID {
			matched = entry.event
			found = true
			continue
		}
		kept = append(kept, entry)
	}
	e.entries = kept
	return matched, found
}
