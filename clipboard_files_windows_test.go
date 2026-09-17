package main

import (
	"encoding/binary"
	"testing"
)

func TestEncodeHDrop(t *testing.T) {
	paths := []string{`C:\Users\Alice\one.txt`, `D:\two.txt`}
	data, err := encodeHDrop(paths)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(data[0:4]) != 20 || binary.LittleEndian.Uint32(data[16:20]) != 1 {
		t.Fatalf("invalid DROPFILES header: %v", data[:20])
	}
	got, err := decodeHDrop(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(paths) || got[0] != paths[0] || got[1] != paths[1] {
		t.Fatalf("decoded paths = %q, want %q", got, paths)
	}
}

func TestDecodeHDropRejectsMalformedData(t *testing.T) {
	valid, err := encodeHDrop([]string{`C:\one.txt`})
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string][]byte{
		"short header":       make([]byte, 19),
		"offset in header":   append([]byte(nil), valid...),
		"offset past end":    append([]byte(nil), valid...),
		"ANSI list":          append([]byte(nil), valid...),
		"odd UTF-16 payload": append(append([]byte(nil), valid...), 1),
		"missing terminator": append([]byte(nil), valid[:len(valid)-2]...),
	}
	binary.LittleEndian.PutUint32(tests["offset in header"][0:4], 4)
	binary.LittleEndian.PutUint32(tests["offset past end"][0:4], uint32(len(valid)+2))
	binary.LittleEndian.PutUint32(tests["ANSI list"][16:20], 0)
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeHDrop(data); err == nil {
				t.Fatal("decodeHDrop accepted malformed data")
			}
		})
	}
}
