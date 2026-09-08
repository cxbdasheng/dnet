package update

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func checksumLine(name string, data []byte) string {
	return fmt.Sprintf("%x  %s\n", sha256.Sum256(data), name)
}

func TestVerifyArchiveChecksumGoodAndTampered(t *testing.T) {
	name := "dnet_1.2.3_linux_x86_64.tar.gz"
	archive := []byte("archive bytes")
	checksums := []byte(checksumLine(name, archive))
	if err := verifyArchiveChecksum(checksums, name, archive); err != nil {
		t.Fatalf("verifyArchiveChecksum() error = %v", err)
	}
	if err := verifyArchiveChecksum(checksums, name, []byte("tampered")); err == nil {
		t.Fatal("verifyArchiveChecksum() accepted tampered archive")
	}
}

func TestVerifyArchiveChecksumMissingAndDuplicate(t *testing.T) {
	name := "dnet_1.2.3_linux_x86_64.tar.gz"
	archive := []byte("archive bytes")
	if err := verifyArchiveChecksum([]byte(checksumLine("other.tar.gz", archive)), name, archive); err == nil {
		t.Fatal("verifyArchiveChecksum() accepted missing archive entry")
	}
	duplicate := checksumLine(name, archive) + checksumLine(name, archive)
	if err := verifyArchiveChecksum([]byte(duplicate), name, archive); err == nil {
		t.Fatal("verifyArchiveChecksum() accepted duplicate archive entry")
	}
}

func TestParseChecksumsAcceptsCRLF(t *testing.T) {
	archive := []byte("archive bytes")
	name := "dnet_1.2.3_windows_x86_64.zip"
	checksums := []byte(strings.ReplaceAll(checksumLine(name, archive), "\n", "\r\n"))
	if err := verifyArchiveChecksum(checksums, name, archive); err != nil {
		t.Fatalf("verifyArchiveChecksum() error = %v", err)
	}
}

func TestParseChecksumsRejectsInvalidLines(t *testing.T) {
	hash := strings.Repeat("a", 64)
	tests := []struct {
		name string
		text string
	}{
		{name: "short hash", text: strings.Repeat("a", 63) + "  dnet.tar.gz\n"},
		{name: "non hex", text: strings.Repeat("g", 64) + "  dnet.tar.gz\n"},
		{name: "one space", text: hash + " dnet.tar.gz\n"},
		{name: "three spaces", text: hash + "   dnet.tar.gz\n"},
		{name: "slash path", text: hash + "  dist/dnet.tar.gz\n"},
		{name: "parent path", text: hash + "  ../dnet.tar.gz\n"},
		{name: "windows path", text: hash + "  dist\\dnet.zip\n"},
		{name: "blank line", text: hash + "  dnet.tar.gz\n\n"},
		{name: "lone carriage return", text: hash + "  dnet.tar.gz\r"},
		{name: "empty", text: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseChecksums([]byte(tt.text)); err == nil {
				t.Fatalf("parseChecksums() accepted %q", tt.text)
			}
		})
	}
}
