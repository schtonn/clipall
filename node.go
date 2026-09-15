package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cespare/xxhash/v2"
)

type Node struct {
	listenPort  int
	peers       []*Peer
	incoming    chan Message
	ring        RingBuffer
	echoes      EchoBuffer
	source      string
	lastEventTS int64
	imageDir    string // if set, save incoming images to this directory
	imageMaxMB  int    // max total size of saved images in MB (0 = unlimited)
}

func NewNode(listenPort int, peerAddrs []string, imageDir string, imageMaxMB int) *Node {
	source, err := os.Hostname()
	if err != nil || source == "" {
		source = "unknown"
	}
	n := &Node{
		listenPort: listenPort,
		incoming:   make(chan Message, 32),
		source:     source,
		imageDir:   imageDir,
		imageMaxMB: imageMaxMB,
	}
	for _, addr := range peerAddrs {
		n.peers = append(n.peers, NewPeer(addr))
	}
	return n
}

// newMessage gives each local clipboard event a stable origin and a strictly
// increasing timestamp. Re-copying identical content therefore remains a new
// event instead of being discarded as a duplicate.
func (n *Node) newMessage(typ MessageType, data []byte) Message {
	timestamp := time.Now().UnixNano()
	if timestamp <= n.lastEventTS {
		timestamp = n.lastEventTS + 1
	}
	n.lastEventTS = timestamp
	return Message{
		Type:      typ,
		ContentID: xxhash.Sum64(data),
		Source:    n.source,
		Timestamp: timestamp,
		Payload:   data,
	}
}

// Run starts the node: listener, peer connections, clipboard watcher, and the
// main event loop. Blocks until ctx is cancelled.
func (n *Node) Run(ctx context.Context) error {
	if err := initClipboard(); err != nil {
		return fmt.Errorf("clipboard init: %w", err)
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", n.listenPort))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()
	log.Printf("[node] listening on :%d", n.listenPort)

	// Accept incoming connections (peers dialing us).
	go n.acceptLoop(ctx, listener)

	// Dial all configured peers (our outgoing connections for sending).
	for _, p := range n.peers {
		go p.Run(ctx)
	}

	// Watch local clipboard for changes (text and image).
	clipCh := watchText(ctx)
	imgCh := watchImage(ctx)
	log.Printf("[node] watching clipboard (text+image), %d peer(s) configured", len(n.peers))

	// Main event loop.
	for {
		select {
		case <-ctx.Done():
			for _, p := range n.peers {
				p.Close()
			}
			return nil

		case data := <-clipCh:
			if len(data) == 0 {
				continue
			}
			id := xxhash.Sum64(data)
			if event, ok := n.echoes.Consume(TypeText, id, time.Now()); ok {
				log.Printf("[node] ignoring expected clipboard echo, hash=%016x, source=%q, timestamp=%d",
					id, event.Source, event.Timestamp)
				continue
			}
			msg := n.newMessage(TypeText, data)
			n.ring.Add(msg.EventID())
			for _, p := range n.peers {
				p.Send(msg)
			}
			log.Printf("[node] sent %d bytes to %d peer(s), hash=%016x, preview=%s",
				len(data), len(n.peers), id, debugPreview(data))

		case data := <-imgCh:
			if len(data) == 0 {
				continue
			}
			id := xxhash.Sum64(data)
			if event, ok := n.echoes.Consume(TypeImage, id, time.Now()); ok {
				log.Printf("[node] ignoring expected image echo, hash=%016x, source=%q, timestamp=%d",
					id, event.Source, event.Timestamp)
				continue
			}
			msg := n.newMessage(TypeImage, data)
			n.ring.Add(msg.EventID())
			for _, p := range n.peers {
				p.Send(msg)
			}
			log.Printf("[node] sent image %d bytes to %d peer(s), hash=%016x",
				len(data), len(n.peers), id)

		case msg := <-n.incoming:
			event := msg.EventID()
			if n.ring.Contains(event) {
				log.Printf("[node] ignoring incoming duplicate, hash=%016x, source=%q, timestamp=%d",
					msg.ContentID, msg.Source, msg.Timestamp)
				continue
			}
			n.ring.Add(event)
			switch msg.Type {
			case TypeText:
				writeText(msg.Payload)
				// Verify: read back and check if clipboard matches what we wrote.
				readback := readText()
				echoID := msg.ContentID
				if len(readback) > 0 {
					echoID = xxhash.Sum64(readback)
				}
				n.echoes.Add(TypeText, echoID, event, time.Now())
				if string(readback) == string(msg.Payload) {
					log.Printf("[node] received %d bytes, write verified OK, hash=%016x, preview=%s",
						len(msg.Payload), msg.ContentID, debugPreview(msg.Payload))
				} else {
					log.Printf("[node] WARNING: clipboard write mismatch! wrote %d bytes, read back %d bytes",
						len(msg.Payload), len(readback))
					log.Printf("[node]   wrote:    %s", debugHex(msg.Payload))
					log.Printf("[node]   readback: %s", debugHex(readback))
				}
			case TypeImage:
				writeImage(msg.Payload)
				// Image readback will differ cross-platform (PNG→DIB→PNG re-encoding
				// on Windows produces different bytes). Track that exact readback as
				// an expected echo; don't treat the mismatch as an error.
				readback := readImage()
				rbHash := msg.ContentID
				if len(readback) > 0 {
					rbHash = xxhash.Sum64(readback)
				}
				n.echoes.Add(TypeImage, rbHash, event, time.Now())
				log.Printf("[node] received image %d bytes, hash=%016x, wrote to clipboard (%d bytes readback)",
					len(msg.Payload), msg.ContentID, len(readback))

				if n.imageDir != "" {
					if path, err := n.saveImage(msg.Payload); err != nil {
						log.Printf("[node] failed to save image: %v", err)
					} else {
						log.Printf("[node] saved image to %s", path)
					}
				}
			default:
				log.Printf("[node] ignoring message type 0x%02x", msg.Type)
			}
		}
	}
}

// acceptLoop accepts incoming TCP connections and starts a reader for each.
func (n *Node) acceptLoop(ctx context.Context, listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[node] accept: %v", err)
			continue
		}
		if tc, ok := conn.(*net.TCPConn); ok {
			tc.SetKeepAlive(true)
			tc.SetKeepAlivePeriod(10 * time.Second)
		}
		log.Printf("[node] accepted connection from %s", conn.RemoteAddr())
		go n.handleConn(ctx, conn)
	}
}

// handleConn reads messages from an accepted connection until it closes or errors.
func (n *Node) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	for {
		msg, err := Decode(conn)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[node] connection from %s closed: %v", conn.RemoteAddr(), err)
			return
		}
		select {
		case n.incoming <- msg:
		case <-ctx.Done():
			return
		}
	}
}

// saveImage writes PNG data to imageDir with a timestamped filename, returning the path.
// Also writes latest.png for convenience. Old images are pruned if imageMaxMB is set.
func (n *Node) saveImage(data []byte) (string, error) {
	if err := os.MkdirAll(n.imageDir, 0755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", n.imageDir, err)
	}
	name := time.Now().Format("20060102-150405.000") + ".png"
	path := filepath.Join(n.imageDir, name)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}
	// Also update latest.png for quick access.
	if err := os.WriteFile(filepath.Join(n.imageDir, "latest.png"), data, 0644); err != nil {
		log.Printf("[node] failed to write latest.png: %v", err)
	}

	if n.imageMaxMB > 0 {
		n.pruneImages()
	}
	return path, nil
}

// pruneImages removes the oldest timestamped images when total directory size exceeds imageMaxMB.
func (n *Node) pruneImages() {
	entries, err := os.ReadDir(n.imageDir)
	if err != nil {
		log.Printf("[node] pruneImages: readdir %s: %v", n.imageDir, err)
		return
	}

	type fileInfo struct {
		name    string
		size    int64
		modTime time.Time
	}
	var files []fileInfo
	var totalSize int64
	for _, e := range entries {
		if e.IsDir() || e.Name() == "latest.png" || filepath.Ext(e.Name()) != ".png" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		totalSize += info.Size()
		files = append(files, fileInfo{name: e.Name(), size: info.Size(), modTime: info.ModTime()})
	}

	maxBytes := int64(n.imageMaxMB) * 1024 * 1024
	if totalSize <= maxBytes {
		return
	}

	// Sort oldest first.
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.Before(files[j].modTime)
	})

	for _, f := range files {
		if totalSize <= maxBytes {
			break
		}
		path := filepath.Join(n.imageDir, f.name)
		if err := os.Remove(path); err == nil {
			log.Printf("[node] pruned old image %s (%d bytes)", f.name, f.size)
			totalSize -= f.size
		}
	}
}

// debugPreview returns the first 40 chars of data for logging.
func debugPreview(data []byte) string {
	s := string(data)
	if len(s) > 40 {
		return fmt.Sprintf("%q...", s[:40])
	}
	return fmt.Sprintf("%q", s)
}

// debugHex returns a hex dump of the first 32 bytes.
func debugHex(data []byte) string {
	if len(data) > 32 {
		return hex.EncodeToString(data[:32]) + "..."
	}
	return hex.EncodeToString(data)
}
