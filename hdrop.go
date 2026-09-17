package main

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"
)

const dropFilesHeaderSize = 20

// encodeHDrop creates the Windows CF_HDROP payload used for file clipboard
// selections. DROPFILES is followed by a double-NUL-terminated UTF-16 list.
func encodeHDrop(paths []string) ([]byte, error) {
	words := make([]uint16, 0)
	for _, path := range paths {
		if path == "" || strings.ContainsRune(path, 0) {
			return nil, fmt.Errorf("invalid clipboard file path")
		}
		words = append(words, utf16.Encode([]rune(path))...)
		words = append(words, 0)
	}
	words = append(words, 0)
	data := make([]byte, dropFilesHeaderSize+len(words)*2)
	binary.LittleEndian.PutUint32(data[0:4], dropFilesHeaderSize)
	binary.LittleEndian.PutUint32(data[16:20], 1) // DROPFILES.fWide
	for i, word := range words {
		binary.LittleEndian.PutUint16(data[dropFilesHeaderSize+i*2:], word)
	}
	return data, nil
}

// decodeHDrop parses a copied CF_HDROP allocation without passing an
// untrusted clipboard handle into shell32.dll.
func decodeHDrop(data []byte) ([]string, error) {
	if len(data) < dropFilesHeaderSize {
		return nil, fmt.Errorf("DROPFILES header is truncated")
	}
	offset := int(binary.LittleEndian.Uint32(data[0:4]))
	if offset < dropFilesHeaderSize || offset > len(data) {
		return nil, fmt.Errorf("invalid file-list offset %d", offset)
	}
	if binary.LittleEndian.Uint32(data[16:20]) == 0 {
		return nil, fmt.Errorf("ANSI CF_HDROP is not supported")
	}
	payload := data[offset:]
	if len(payload) < 2 || len(payload)%2 != 0 {
		return nil, fmt.Errorf("UTF-16 file list has invalid length")
	}
	words := make([]uint16, len(payload)/2)
	for i := range words {
		words[i] = binary.LittleEndian.Uint16(payload[i*2:])
	}
	paths := make([]string, 0)
	start := 0
	terminated := false
	for i, word := range words {
		if word != 0 {
			continue
		}
		if i == start {
			terminated = true
			break
		}
		if len(paths) >= maxFileEntries {
			return nil, fmt.Errorf("file list exceeds %d entries", maxFileEntries)
		}
		path := string(utf16.Decode(words[start:i]))
		if path == "" || strings.ContainsRune(path, 0) {
			return nil, fmt.Errorf("invalid file path")
		}
		paths = append(paths, path)
		start = i + 1
	}
	if !terminated {
		return nil, fmt.Errorf("file list is not double-NUL terminated")
	}
	return paths, nil
}
