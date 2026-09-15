//go:build windows

package main

import (
	"encoding/binary"
	"syscall"
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
	words := make([]uint16, (len(data)-20)/2)
	for i := range words {
		words[i] = binary.LittleEndian.Uint16(data[20+i*2:])
	}
	var got []string
	start := 0
	for i, word := range words {
		if word != 0 {
			continue
		}
		if i == start {
			break
		}
		got = append(got, syscall.UTF16ToString(words[start:i]))
		start = i + 1
	}
	if len(got) != len(paths) || got[0] != paths[0] || got[1] != paths[1] {
		t.Fatalf("decoded paths = %q, want %q", got, paths)
	}
}
