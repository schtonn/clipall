package main

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileProviderCreatesLazyDirectoryOffer(t *testing.T) {
	root := filepath.Join(t.TempDir(), "folder")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	content := []byte("contents are fetched later")
	if err := os.WriteFile(filepath.Join(root, "sub", "file.txt"), content, 0600); err != nil {
		t.Fatal(err)
	}

	var provider FileProvider
	offer, err := provider.Create([]string{root}, "100.64.0.1:9876")
	if err != nil {
		t.Fatal(err)
	}
	if len(offer.Roots) != 1 || offer.Roots[0] != "folder" || len(offer.Entries) != 3 {
		t.Fatalf("unexpected offer: %+v", offer)
	}
	if err := validateFileOffer(offer); err != nil {
		t.Fatalf("valid offer rejected: %v", err)
	}

	fileIndex := -1
	for i, entry := range offer.Entries {
		if entry.Path == "folder/sub/file.txt" {
			fileIndex = i
		}
	}
	if fileIndex < 0 {
		t.Fatal("offered file not found")
	}
	file, size, err := provider.open(FileFetchRequest{OfferID: offer.ID, Index: fileIndex})
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	got, err := io.ReadAll(file)
	if err != nil || size != int64(len(content)) || string(got) != string(content) {
		t.Fatalf("fetched size=%d data=%q err=%v", size, got, err)
	}
}

func TestFileProviderRejectsChangedSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	var provider FileProvider
	offer, err := provider.Create([]string{path}, "100.64.0.1:9876")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if err := os.WriteFile(path, []byte("after, with a different size"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := provider.open(FileFetchRequest{OfferID: offer.ID, Index: 0}); err == nil {
		t.Fatal("changed source file should be rejected")
	}
}

func TestFileProviderServesRequestedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.bin")
	content := []byte{1, 2, 3, 4, 5}
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	var provider FileProvider
	offer, err := provider.Create([]string{path}, "100.64.0.1:9876")
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(FileFetchRequest{OfferID: offer.ID, Index: 0})
	server, client := net.Pipe()
	defer client.Close()
	go func() {
		defer server.Close()
		provider.Serve(server, payload)
	}()
	header := make([]byte, 9)
	if _, err := io.ReadFull(client, header); err != nil {
		t.Fatal(err)
	}
	if header[0] != fileFetchOK || int64(binary.BigEndian.Uint64(header[1:])) != int64(len(content)) {
		t.Fatalf("unexpected response header: %v", header)
	}
	got, err := io.ReadAll(client)
	if err != nil || string(got) != string(content) {
		t.Fatalf("served data=%v err=%v", got, err)
	}
}

func TestValidateFileOfferRejectsTraversalAndNonTailscaleSource(t *testing.T) {
	base := FileOffer{
		Version: fileOfferVersion,
		ID:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Address: "100.64.0.1:9876",
		Expires: time.Now().Add(time.Hour).Unix(),
		Roots:   []string{"root"},
		Entries: []FileEntry{{Path: "root/file.txt", Size: 1}},
	}
	badPath := base
	badPath.Entries = []FileEntry{{Path: "../escape.txt", Size: 1}}
	if err := validateFileOffer(badPath); err == nil {
		t.Fatal("path traversal should be rejected")
	}
	badAddress := base
	badAddress.Address = "127.0.0.1:9876"
	if err := validateFileOffer(badAddress); err == nil {
		t.Fatal("non-Tailscale source should be rejected")
	}
	if !isTailscaleAddr(netip.MustParseAddr("100.100.100.100")) || !isTailscaleAddr(netip.MustParseAddr("fd7a:115c:a1e0::1")) {
		t.Fatal("Tailscale address ranges were not recognized")
	}
}

func TestValidateRelativeOfferPathRejectsWindowsSpecialNames(t *testing.T) {
	for _, path := range []string{"root/CON.txt", "root/trailing. ", "root/a:b.txt", "root/question?.txt"} {
		if err := validateRelativeOfferPath(path); err == nil {
			t.Errorf("unsafe Windows path %q was accepted", path)
		}
	}
	for _, path := range []string{"root/普通文件.txt", "root/a b/file.txt"} {
		if err := validateRelativeOfferPath(path); err != nil {
			t.Errorf("valid path %q was rejected: %v", path, err)
		}
	}
}
