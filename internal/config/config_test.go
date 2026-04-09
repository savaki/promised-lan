package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyDefaults(t *testing.T) {
	var c Config
	c.applyDefaults()
	if c.UpstreamInterface != "wlan1" {
		t.Errorf("UpstreamInterface: got %q, want wlan1", c.UpstreamInterface)
	}
	if c.InteriorInterface != "wlan0" {
		t.Errorf("InteriorInterface: got %q, want wlan0", c.InteriorInterface)
	}
	if c.HTTPPort != 8080 {
		t.Errorf("HTTPPort: got %d, want 8080", c.HTTPPort)
	}
	if c.ConnectionName != "promised-lan-upstream" {
		t.Errorf("ConnectionName: got %q, want promised-lan-upstream", c.ConnectionName)
	}
	if c.ConnectTimeoutSeconds != 20 {
		t.Errorf("ConnectTimeoutSeconds: got %d, want 20", c.ConnectTimeoutSeconds)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string // substring; empty = expect no error
	}{
		{
			name: "valid",
			cfg: Config{
				UpstreamInterface:     "wlan1",
				InteriorInterface:     "wlan0",
				HTTPPort:              8080,
				ConnectionName:        "promised-lan-upstream",
				ConnectTimeoutSeconds: 20,
			},
		},
		{
			name: "same interface",
			cfg: Config{
				UpstreamInterface:     "wlan0",
				InteriorInterface:     "wlan0",
				HTTPPort:              8080,
				ConnectionName:        "x",
				ConnectTimeoutSeconds: 20,
			},
			wantErr: "upstream_interface must not equal interior_interface",
		},
		{
			name: "bad port",
			cfg: Config{
				UpstreamInterface:     "wlan1",
				InteriorInterface:     "wlan0",
				HTTPPort:              70000,
				ConnectionName:        "x",
				ConnectTimeoutSeconds: 20,
			},
			wantErr: "http_port",
		},
		{
			name: "bad timeout",
			cfg: Config{
				UpstreamInterface:     "wlan1",
				InteriorInterface:     "wlan0",
				HTTPPort:              8080,
				ConnectionName:        "x",
				ConnectTimeoutSeconds: 999,
			},
			wantErr: "connect_timeout_seconds",
		},
		{
			name: "bad connection name",
			cfg: Config{
				UpstreamInterface:     "wlan1",
				InteriorInterface:     "wlan0",
				HTTPPort:              8080,
				ConnectionName:        "bad name!",
				ConnectTimeoutSeconds: 20,
			},
			wantErr: "connection_name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
upstream_interface: wlan2
interior_interface: wlan0
http_port: 9090
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.UpstreamInterface != "wlan2" {
		t.Errorf("UpstreamInterface: got %q, want wlan2", cfg.UpstreamInterface)
	}
	if cfg.HTTPPort != 9090 {
		t.Errorf("HTTPPort: got %d, want 9090", cfg.HTTPPort)
	}
	// Defaults applied for missing fields
	if cfg.ConnectionName != "promised-lan-upstream" {
		t.Errorf("ConnectionName default not applied: got %q", cfg.ConnectionName)
	}
	if cfg.ConnectTimeoutSeconds != 20 {
		t.Errorf("ConnectTimeoutSeconds default not applied: got %d", cfg.ConnectTimeoutSeconds)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestValidateAggregatesAllErrors(t *testing.T) {
	cfg := Config{
		UpstreamInterface:     "wlan0",
		InteriorInterface:     "wlan0",
		HTTPPort:              70000,
		ConnectionName:        "bad name!",
		ConnectTimeoutSeconds: 999,
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"upstream_interface must not equal interior_interface",
		"http_port",
		"connect_timeout_seconds",
		"connection_name",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("aggregated error missing %q: %s", want, msg)
		}
	}
}

func TestLoadMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("not: valid: yaml: ::"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected parse error for malformed YAML")
	}
}
