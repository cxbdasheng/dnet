package update

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateWithClientReplacesOnlyVerifiedExecutable(t *testing.T) {
	archiveName := "dnet_1.2.3_linux_x86_64.tar.gz"
	archive, err := createTestTarGz(map[string]string{"dnet": "new executable"})
	if err != nil {
		t.Fatalf("createTestTarGz() error = %v", err)
	}
	archiveBytes := archive.Bytes()
	checksumBytes := []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(archiveBytes), archiveName))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/checksums.txt":
			_, _ = w.Write(checksumBytes)
		case "/" + archiveName:
			_, _ = w.Write(archiveBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	targetPath := filepath.Join(t.TempDir(), "dnet")
	if err := os.WriteFile(targetPath, []byte("old executable"), 0o751); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	release := &Release{
		Version:   &Version{major: 1, minor: 2, patch: 3},
		Archive:   Asset{Name: archiveName, URL: server.URL + "/" + archiveName},
		Checksums: Asset{Name: "checksums.txt", URL: server.URL + "/checksums.txt"},
	}

	if err := updateWithClient(server.Client(), release, targetPath); err != nil {
		t.Fatalf("updateWithClient() error = %v", err)
	}
	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "new executable" {
		t.Fatalf("updated executable = %q, want %q", got, "new executable")
	}
	info, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o751 {
		t.Fatalf("updated mode = %o, want 751", info.Mode().Perm())
	}
}

func TestUpdateWithClientChecksumMismatchDoesNotReplaceExecutable(t *testing.T) {
	archiveName := "dnet_1.2.3_linux_x86_64.tar.gz"
	archive, err := createTestTarGz(map[string]string{"dnet": "new executable"})
	if err != nil {
		t.Fatalf("createTestTarGz() error = %v", err)
	}
	archiveBytes := archive.Bytes()
	wrongChecksum := sha256.Sum256([]byte("different archive"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/checksums.txt":
			_, _ = fmt.Fprintf(w, "%x  %s\n", wrongChecksum, archiveName)
		case "/" + archiveName:
			_, _ = w.Write(archiveBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	targetPath := filepath.Join(t.TempDir(), "dnet")
	const oldExecutable = "old executable"
	if err := os.WriteFile(targetPath, []byte(oldExecutable), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	release := &Release{
		Version:   &Version{major: 1, minor: 2, patch: 3},
		Archive:   Asset{Name: archiveName, URL: server.URL + "/" + archiveName},
		Checksums: Asset{Name: "checksums.txt", URL: server.URL + "/checksums.txt"},
	}

	if err := updateWithClient(server.Client(), release, targetPath); err == nil {
		t.Fatal("updateWithClient() error = nil, want checksum mismatch")
	}
	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != oldExecutable {
		t.Fatalf("executable changed after checksum failure: %q", got)
	}
	for _, unwanted := range []string{targetPath + ".new", targetPath + ".old"} {
		if _, err := os.Stat(unwanted); !os.IsNotExist(err) {
			t.Fatalf("temporary update file exists after checksum failure: %s", unwanted)
		}
	}
}

func TestUpdateWithClientReplacesFromVerifiedZip(t *testing.T) {
	archiveName := "dnet_1.2.3_windows_x86_64.zip"
	archive, err := createTestZip(map[string]string{"dnet.exe": "new windows executable"})
	if err != nil {
		t.Fatalf("createTestZip() error = %v", err)
	}
	archiveBytes := archive.Bytes()
	checksums := []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(archiveBytes), archiveName))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/checksums.txt" {
			_, _ = w.Write(checksums)
			return
		}
		_, _ = w.Write(archiveBytes)
	}))
	defer server.Close()

	targetPath := filepath.Join(t.TempDir(), "dnet")
	if err := os.WriteFile(targetPath, []byte("old executable"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	release := &Release{
		Archive:   Asset{Name: archiveName, URL: server.URL + "/" + archiveName},
		Checksums: Asset{Name: "checksums.txt", URL: server.URL + "/checksums.txt"},
	}
	if err := updateWithClient(server.Client(), release, targetPath); err != nil {
		t.Fatalf("updateWithClient() error = %v", err)
	}
	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "new windows executable" {
		t.Fatalf("updated executable = %q", got)
	}
}

func TestDownloadBytesRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "9")
		_, _ = w.Write([]byte("123456789"))
	}))
	defer server.Close()

	_, err := downloadBytes(server.Client(), server.URL, 8)
	if err == nil || !strings.Contains(err.Error(), "超过") {
		t.Fatalf("downloadBytes() error = %v, want size limit error", err)
	}
}

func TestDownloadArchiveRejectsChunkedOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Trailer", "X-Test-Trailer")
		_, _ = w.Write([]byte("123456789"))
	}))
	defer server.Close()

	archive, _, err := downloadArchive(server.Client(), server.URL, 8)
	if archive != nil {
		name := archive.Name()
		_ = archive.Close()
		_ = os.Remove(name)
		t.Fatal("downloadArchive() returned an archive for an oversized response")
	}
	if err == nil || !strings.Contains(err.Error(), "超过") {
		t.Fatalf("downloadArchive() error = %v, want size limit error", err)
	}
}

func TestUpdateWithClientInvalidChecksumsDoNotReplaceExecutable(t *testing.T) {
	archiveName := "dnet_1.2.3_linux_x86_64.tar.gz"
	archive, err := createTestTarGz(map[string]string{"dnet": "new executable"})
	if err != nil {
		t.Fatalf("createTestTarGz() error = %v", err)
	}
	archiveBytes := archive.Bytes()
	digest := sha256.Sum256(archiveBytes)

	tests := []struct {
		name      string
		checksums string
	}{
		{name: "missing", checksums: fmt.Sprintf("%x  other.tar.gz\n", digest)},
		{name: "duplicate", checksums: fmt.Sprintf("%x  %s\n%x  %s\n", digest, archiveName, digest, archiveName)},
		{name: "invalid", checksums: "not-a-checksum\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/checksums.txt" {
					_, _ = w.Write([]byte(tt.checksums))
					return
				}
				_, _ = w.Write(archiveBytes)
			}))
			defer server.Close()

			targetPath := filepath.Join(t.TempDir(), "dnet")
			const oldExecutable = "old executable"
			if err := os.WriteFile(targetPath, []byte(oldExecutable), 0o751); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			release := &Release{
				Archive:   Asset{Name: archiveName, URL: server.URL + "/" + archiveName},
				Checksums: Asset{Name: "checksums.txt", URL: server.URL + "/checksums.txt"},
			}

			if err := updateWithClient(server.Client(), release, targetPath); err == nil {
				t.Fatal("updateWithClient() error = nil, want checksum error")
			}
			got, err := os.ReadFile(targetPath)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			if string(got) != oldExecutable {
				t.Fatalf("executable changed after checksum failure: %q", got)
			}
			info, err := os.Stat(targetPath)
			if err != nil {
				t.Fatalf("Stat() error = %v", err)
			}
			if info.Mode().Perm() != 0o751 {
				t.Fatalf("executable mode changed after checksum failure: %o", info.Mode().Perm())
			}
		})
	}
}
