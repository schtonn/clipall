package main

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

type failingConn struct{}

func (failingConn) Read([]byte) (int, error)         { return 0, io.ErrClosedPipe }
func (failingConn) Write([]byte) (int, error)        { return 0, io.ErrClosedPipe }
func (failingConn) Close() error                     { return nil }
func (failingConn) LocalAddr() net.Addr              { return dummyAddr("local") }
func (failingConn) RemoteAddr() net.Addr             { return dummyAddr("remote") }
func (failingConn) SetDeadline(time.Time) error      { return nil }
func (failingConn) SetReadDeadline(time.Time) error  { return nil }
func (failingConn) SetWriteDeadline(time.Time) error { return nil }

type dummyAddr string

func (a dummyAddr) Network() string { return "test" }
func (a dummyAddr) String() string  { return string(a) }

func TestWriteLoopReturnsFailedMessageForReconnect(t *testing.T) {
	p := NewPeer("peer:9876")
	p.conn = failingConn{}
	want := Message{Type: TypeText, ContentID: 42, Payload: []byte("retry me")}
	p.Send(want)

	pending := p.writeLoop(context.Background(), nil)
	if pending == nil || pending.ContentID != want.ContentID || string(pending.Payload) != string(want.Payload) {
		t.Fatalf("pending message = %+v, want %+v", pending, want)
	}
}
