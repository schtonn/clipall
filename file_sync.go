package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	fileOfferVersion  = 1
	fileOfferLifetime = 24 * time.Hour
	maxFileEntries    = 4096
	fileFetchOK       = byte(0)
	fileFetchError    = byte(1)
)

type FileEntry struct {
	Path      string `json:"path"`
	Size      int64  `json:"size,omitempty"`
	ModTimeNS int64  `json:"modified_ns,omitempty"`
	Directory bool   `json:"directory,omitempty"`
}

type FileOffer struct {
	Version int         `json:"version"`
	ID      string      `json:"id"`
	Address string      `json:"address"`
	Expires int64       `json:"expires"`
	Roots   []string    `json:"roots"`
	Entries []FileEntry `json:"entries"`
}

type FileFetchRequest struct {
	OfferID string `json:"offer_id"`
	Index   int    `json:"index"`
}

type providedFile struct {
	entry FileEntry
	path  string
}

type FileProvider struct {
	mu      sync.RWMutex
	id      string
	expires time.Time
	files   []providedFile
}

func (p *FileProvider) Clear() {
	p.mu.Lock()
	p.id = ""
	p.expires = time.Time{}
	p.files = nil
	p.mu.Unlock()
}

func (p *FileProvider) Create(paths []string, address string) (FileOffer, error) {
	p.Clear()
	if len(paths) == 0 {
		return FileOffer{}, fmt.Errorf("empty file selection")
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return FileOffer{}, fmt.Errorf("create offer token: %w", err)
	}
	id := hex.EncodeToString(token)
	expires := time.Now().Add(fileOfferLifetime)

	seenRoots := make(map[string]bool)
	var roots []string
	var files []providedFile
	for _, selected := range paths {
		absolute, err := filepath.Abs(selected)
		if err != nil {
			return FileOffer{}, fmt.Errorf("resolve selected path: %w", err)
		}
		rootName := filepath.Base(filepath.Clean(absolute))
		if rootName == "." || rootName == string(filepath.Separator) || rootName == "" {
			return FileOffer{}, fmt.Errorf("cannot offer filesystem root %q", selected)
		}
		rootKey := strings.ToLower(rootName)
		if seenRoots[rootKey] {
			return FileOffer{}, fmt.Errorf("selected items have duplicate root name %q", rootName)
		}
		seenRoots[rootKey] = true
		roots = append(roots, rootName)

		err = filepath.WalkDir(absolute, func(path string, dirEntry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if len(files) >= maxFileEntries {
				return fmt.Errorf("selection exceeds %d entries", maxFileEntries)
			}
			info, err := dirEntry.Info()
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("symbolic links are not supported: %s", path)
			}
			if !info.IsDir() && !info.Mode().IsRegular() {
				return fmt.Errorf("special files are not supported: %s", path)
			}
			rel, err := filepath.Rel(absolute, path)
			if err != nil {
				return err
			}
			offerPath := rootName
			if rel != "." {
				offerPath = filepath.Join(rootName, rel)
			}
			entry := FileEntry{
				Path:      filepath.ToSlash(offerPath),
				Size:      info.Size(),
				ModTimeNS: info.ModTime().UnixNano(),
				Directory: info.IsDir(),
			}
			files = append(files, providedFile{entry: entry, path: path})
			return nil
		})
		if err != nil {
			return FileOffer{}, fmt.Errorf("inspect %s: %w", selected, err)
		}
	}

	entries := make([]FileEntry, len(files))
	for i := range files {
		entries[i] = files[i].entry
	}
	offer := FileOffer{
		Version: fileOfferVersion,
		ID:      id,
		Address: address,
		Expires: expires.Unix(),
		Roots:   roots,
		Entries: entries,
	}
	p.mu.Lock()
	p.id = id
	p.expires = expires
	p.files = files
	p.mu.Unlock()
	return offer, nil
}

func (p *FileProvider) open(request FileFetchRequest) (*os.File, int64, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if request.OfferID == "" || request.OfferID != p.id || time.Now().After(p.expires) {
		return nil, 0, fmt.Errorf("file offer is unavailable or expired")
	}
	if request.Index < 0 || request.Index >= len(p.files) {
		return nil, 0, fmt.Errorf("file index out of range")
	}
	provided := p.files[request.Index]
	if provided.entry.Directory {
		return nil, 0, fmt.Errorf("cannot fetch a directory")
	}
	file, err := os.Open(provided.path)
	if err != nil {
		return nil, 0, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, 0, err
	}
	if !info.Mode().IsRegular() || info.Size() != provided.entry.Size || info.ModTime().UnixNano() != provided.entry.ModTimeNS {
		file.Close()
		return nil, 0, fmt.Errorf("source file changed after it was copied")
	}
	return file, info.Size(), nil
}

func (p *FileProvider) Serve(conn net.Conn, payload []byte) {
	var request FileFetchRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		writeFileFetchHeader(conn, fileFetchError, 0)
		return
	}
	file, size, err := p.open(request)
	if err != nil {
		writeFileFetchHeader(conn, fileFetchError, 0)
		return
	}
	defer file.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(fileOfferLifetime))
	if err := writeFileFetchHeader(conn, fileFetchOK, size); err != nil {
		return
	}
	_, _ = io.CopyN(conn, file, size)
}

func writeFileFetchHeader(writer io.Writer, status byte, size int64) error {
	header := make([]byte, 9)
	header[0] = status
	binary.BigEndian.PutUint64(header[1:], uint64(size))
	_, err := writer.Write(header)
	return err
}

func validateFileOffer(offer FileOffer) error {
	if offer.Version != fileOfferVersion || len(offer.ID) != 64 || len(offer.Entries) == 0 || len(offer.Entries) > maxFileEntries {
		return fmt.Errorf("invalid file offer metadata")
	}
	if _, err := hex.DecodeString(offer.ID); err != nil {
		return fmt.Errorf("invalid file offer ID")
	}
	if time.Now().Unix() >= offer.Expires {
		return fmt.Errorf("file offer has expired")
	}
	host, _, err := net.SplitHostPort(offer.Address)
	if err != nil {
		return fmt.Errorf("invalid source address: %w", err)
	}
	ip, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil || !isTailscaleAddr(ip) {
		return fmt.Errorf("file source is not a Tailscale address")
	}
	rootSet := make(map[string]bool)
	for _, root := range offer.Roots {
		if err := validateRelativeOfferPath(root); err != nil || strings.Contains(root, "/") || strings.Contains(root, "\\") {
			return fmt.Errorf("invalid file offer root %q", root)
		}
		rootSet[root] = true
	}
	for _, entry := range offer.Entries {
		if entry.Size < 0 || validateRelativeOfferPath(entry.Path) != nil {
			return fmt.Errorf("invalid file entry %q", entry.Path)
		}
		root := strings.Split(entry.Path, "/")[0]
		if !rootSet[root] {
			return fmt.Errorf("file entry is outside offered roots")
		}
	}
	return nil
}

func validateRelativeOfferPath(path string) error {
	if path == "" || strings.ContainsRune(path, 0) || strings.Contains(path, "\\") {
		return fmt.Errorf("invalid relative path")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean != path || filepath.IsAbs(filepath.FromSlash(path)) || path == ".." || strings.HasPrefix(path, "../") {
		return fmt.Errorf("unsafe relative path")
	}
	for _, component := range strings.Split(path, "/") {
		if err := validateWindowsPathComponent(component); err != nil {
			return err
		}
	}
	return nil
}

func validateWindowsPathComponent(component string) error {
	if component == "" || strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") || strings.ContainsAny(component, `<>:"|?*`) {
		return fmt.Errorf("path is not compatible with Windows")
	}
	for _, character := range component {
		if character < 32 {
			return fmt.Errorf("path contains a control character")
		}
	}
	base := strings.ToUpper(strings.SplitN(component, ".", 2)[0])
	reserved := base == "CON" || base == "PRN" || base == "AUX" || base == "NUL"
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		reserved = true
	}
	if reserved {
		return fmt.Errorf("path uses a reserved Windows name")
	}
	return nil
}

func receivedStagingSelection(paths []string) bool {
	if len(paths) == 0 {
		return false
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return false
	}
	stagingRoot := filepath.Clean(filepath.Join(cacheDir, "clipall", "files")) + string(filepath.Separator)
	for _, path := range paths {
		clean := filepath.Clean(path)
		if !strings.HasPrefix(strings.ToLower(clean), strings.ToLower(stagingRoot)) {
			return false
		}
	}
	return true
}

func receiveFileOffer(ctx context.Context, offer FileOffer) ([]string, error) {
	if err := validateFileOffer(offer); err != nil {
		return nil, err
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("find cache directory: %w", err)
	}
	stagingRoot := filepath.Join(cacheDir, "clipall", "files")
	pruneFileStaging(stagingRoot, offer.ID)
	staging := filepath.Join(stagingRoot, offer.ID)
	if err := os.MkdirAll(staging, 0700); err != nil {
		return nil, fmt.Errorf("create staging directory: %w", err)
	}
	for index, entry := range offer.Entries {
		target := filepath.Join(staging, filepath.FromSlash(entry.Path))
		if entry.Directory {
			if err := os.MkdirAll(target, 0700); err != nil {
				return nil, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return nil, err
		}
		if info, err := os.Stat(target); err == nil && info.Mode().IsRegular() && info.Size() == entry.Size {
			continue
		}
		if err := fetchOfferedFile(ctx, offer, index, target, entry.Size); err != nil {
			return nil, fmt.Errorf("fetch %s: %w", entry.Path, err)
		}
	}
	rootPaths := make([]string, len(offer.Roots))
	for i, root := range offer.Roots {
		rootPaths[i] = filepath.Join(staging, root)
	}
	return rootPaths, nil
}

func pruneFileStaging(root, keepID string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == keepID {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(filepath.Join(root, entry.Name()))
		}
	}
}

func fetchOfferedFile(ctx context.Context, offer FileOffer, index int, target string, expectedSize int64) error {
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", offer.Address)
	if err != nil {
		return err
	}
	defer conn.Close()
	requestPayload, err := json.Marshal(FileFetchRequest{OfferID: offer.ID, Index: index})
	if err != nil {
		return err
	}
	request, err := Encode(Message{Type: TypeFileFetch, Payload: requestPayload})
	if err != nil {
		return err
	}
	if _, err := conn.Write(request); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(fileOfferLifetime))
	header := make([]byte, 9)
	if _, err := io.ReadFull(conn, header); err != nil {
		return err
	}
	if header[0] != fileFetchOK {
		return fmt.Errorf("source refused the file request")
	}
	size := int64(binary.BigEndian.Uint64(header[1:]))
	if size != expectedSize {
		return fmt.Errorf("source size changed: got %d, expected %d", size, expectedSize)
	}
	part := target + ".part"
	file, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.CopyN(file, conn, size)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(part)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(part)
		return closeErr
	}
	if err := os.Rename(part, target); err != nil {
		_ = os.Remove(part)
		return err
	}
	return nil
}

func fileOfferStats(offer FileOffer) (files int, directories int, bytes int64) {
	for _, entry := range offer.Entries {
		if entry.Directory {
			directories++
		} else {
			files++
			bytes += entry.Size
		}
	}
	return
}
