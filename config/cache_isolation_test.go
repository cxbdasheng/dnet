package config

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestConfigCacheSnapshots(t *testing.T) {
	t.Setenv(PathENV, filepath.Join(t.TempDir(), "config.yaml"))
	original := Config{
		User:       User{Username: "admin"},
		DCDNConfig: DCDNConfig{DCDN: []CDN{{Domain: "example.com", Sources: []Source{{Value: "192.0.2.1"}}}}},
		DDNSConfig: DDNSConfig{DDNS: []DNSGroup{{Domain: "example.com", Records: []DNSRecord{{Value: "192.0.2.2"}}}}},
	}
	mutate := func(c *Config) {
		c.Username = "changed"
		c.DCDN[0].CName = "changed"
		c.DCDN[0].Sources[0].Value = "changed"
		c.DDNS[0].Domain = "changed"
		c.DDNS[0].Records[0].Value = "changed"
	}
	saved := original.clone()
	if err := saved.SaveConfig(); err != nil {
		t.Fatal(err)
	}
	mutate(&saved)
	assertSnapshot := func(c Config, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(c, original) {
			t.Fatalf("snapshot changed: %+v", c)
		}
	}
	assertSnapshot(GetConfigCached())
	loaded, err := GetConfigCached()
	if err != nil {
		t.Fatal(err)
	}
	mutate(&loaded)
	assertSnapshot(GetConfigCached())
	// Check the file-loading path and subsequent cache hits independently.
	cache := &ConfigCache{}
	loaded, err = cache.loadConfig(GetConfigFilePath())
	if err != nil {
		t.Fatal(err)
	}
	mutate(&loaded)
	assertSnapshot(cache.loadConfig(GetConfigFilePath()))
}
