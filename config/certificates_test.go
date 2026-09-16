package config

import (
	"bytes"
	"github.com/cxbdasheng/dnet/certificates"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCertificateConfigCloneAndPrivateWrite(t *testing.T) {
	c := Config{Certificates: []certificates.Certificate{{ID: "one", Domains: []string{"example.com"}, KeyPEM: "private-key"}}}
	copy := c.clone()
	copy.Certificates[0].Domains[0] = "changed"
	if c.Certificates[0].Domains[0] != "example.com" {
		t.Fatal("shared domain slice")
	}
	b, err := yaml.Marshal(c)
	if err != nil || !strings.Contains(string(b), "private-key") {
		t.Fatal("private material not persisted", err)
	}
	var restored Config
	if err = yaml.Unmarshal(b, &restored); err != nil || restored.Certificates[0].KeyPEM != "private-key" {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err = savePrivateConfig(path, b); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written, b) {
		t.Fatal("replacement config content mismatch")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows reports DOS read-only attributes as mode bits, not POSIX permissions.
	// Keep content/replacement checks on every platform and enforce 0600 on Unix.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("config permissions", info.Mode())
	}
}
