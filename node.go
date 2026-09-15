package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cespare/xxhash/v2"
)

type Node struct {
	listenHost  string
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
	return NewNodeAt("tailscale", listenPort, peerAddrs, imageDir, imageMaxMB)
}

func NewNodeAt(listenHost string, listenPort int, peerAddrs []string, imageDir string, imageMaxMB int) *Node {
	source, err := os.Hostname()
	if err != nil || source == "" {
		source = "unknown"
	}
	n := &Node{
		listenHost: listenHost,
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

	listener, err := n.listen(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	defer listener.Close()
	log.Printf("[node] listening on %s", listener.Addr())

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
				log.Printf("[node] ignoring expected clipboard echo from %q at %d",
					event.Source, event.Timestamp)
				continue
			}
			msg := n.newMessage(TypeText, data)
			n.ring.Add(msg.EventID())
			for _, p := range n.peers {
				p.Send(msg)
			}
			log.Printf("[node] sent text (%d bytes) to %d peer(s)", len(data), len(n.peers))

		case data := <-imgCh:
			if len(data) == 0 {
				continue
			}
			id := xxhash.Sum64(data)
			if event, ok := n.echoes.Consume(TypeImage, id, time.Now()); ok {
				log.Printf("[node] ignoring expected image echo from %q at %d",
					event.Source, event.Timestamp)
				continue
			}
			msg := n.newMessage(TypeImage, data)
			n.ring.Add(msg.EventID())
			for _, p := range n.peers {
				p.Send(msg)
			}
			log.Printf("[node] sent image (%d bytes) to %d peer(s)", len(data), len(n.peers))

		case msg := <-n.incoming:
			event := msg.EventID()
			if n.ring.Contains(event) {
				log.Printf("[node] ignoring incoming duplicate from %q at %d",
					msg.Source, msg.Timestamp)
				continue
			}
			switch msg.Type {
			case TypeText:
				if err := writeText(msg.Payload); err != nil {
					log.Printf("[node] failed to write incoming text from %q: %v", msg.Source, err)
					continue
				}
				n.ring.Add(event)
				// Verify: read back and check if clipboard matches what we wrote.
				readback := readText()
				echoID := msg.ContentID
				if len(readback) > 0 {
					echoID = xxhash.Sum64(readback)
				}
				n.echoes.Add(TypeText, echoID, event, time.Now())
				if string(readback) == string(msg.Payload) {
					log.Printf("[node] received text from %q (%d bytes), write verified", msg.Source, len(msg.Payload))
				} else {
					log.Printf("[node] warning: clipboard text write mismatch (wrote %d bytes, read back %d bytes)", len(msg.Payload), len(readback))
				}
			case TypeImage:
				if err := writeImage(msg.Payload); err != nil {
					log.Printf("[node] failed to write incoming image from %q: %v", msg.Source, err)
					continue
				}
				n.ring.Add(event)
				// Image readback will differ cross-platform (PNG→DIB→PNG re-encoding
				// on Windows produces different bytes). Track that exact readback as
				// an expected echo; don't treat the mismatch as an error.
				readback := readImage()
				rbHash := msg.ContentID
				if len(readback) > 0 {
					rbHash = xxhash.Sum64(readback)
				}
				n.echoes.Add(TypeImage, rbHash, event, time.Now())
				log.Printf("[node] received image from %q (%d bytes), wrote to clipboard (%d bytes readback)",
					msg.Source, len(msg.Payload), len(readback))

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

// listen waits for Tailscale to become ready. This matters during login, when
// an autostarted clipall process can run before the Tailscale interface exists.
// Explicit addresses still fail immediately so configuration errors are clear.
func (n *Node) listen(ctx context.Context) (net.Listener, error) {
	for {
		listenHost, err := resolveListenHost(n.listenHost)
		if err == nil {
			listenAddr := net.JoinHostPort(listenHost, fmt.Sprint(n.listenPort))
			listener, listenErr := net.Listen("tcp", listenAddr)
			if listenErr == nil {
				return listener, nil
			}
			err = fmt.Errorf("listen on %s: %w", listenAddr, listenErr)
		}
		if strings.TrimSpace(n.listenHost) != "tailscale" {
			return nil, fmt.Errorf("listen: %w", err)
		}
		log.Printf("[node] waiting for Tailscale before listening: %v", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
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
	if err := os.MkdirAll(n.imageDir, 0700); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", n.imageDir, err)
	}
	var path string
	for suffix := 0; ; suffix++ {
		name := time.Now().Format("20060102-150405.000000000")
		if suffix > 0 {
			name = fmt.Sprintf("%s-%d", name, suffix)
		}
		path = filepath.Join(n.imageDir, name+".png")
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if writeErr != nil {
			_ = os.Remove(path)
			return "", writeErr
		}
		if closeErr != nil {
			_ = os.Remove(path)
			return "", closeErr
		}
		break
	}
	// Also update latest.png for quick access.
	if err := os.WriteFile(filepath.Join(n.imageDir, "latest.png"), data, 0600); err != nil {
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
		if e.IsDir() || filepath.Ext(e.Name()) != ".png" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		totalSize += info.Size()
		if e.Name() != "latest.png" {
			files = append(files, fileInfo{name: e.Name(), size: info.Size(), modTime: info.ModTime()})
		}
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
