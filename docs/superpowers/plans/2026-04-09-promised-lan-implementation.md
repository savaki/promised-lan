# promised-lan Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a self-installing Go binary that manages upstream WiFi credentials on a Raspberry Pi with two radios via NetworkManager, exposing an interior-only password-less web UI.

**Architecture:** Single Go binary with isolated internal packages. All NetworkManager interaction is confined to `internal/nm`, which shells out to `nmcli` through a redacting command runner. The web server binds only to the interior interface's IPv4 (not `0.0.0.0`). Subcommands (`serve`, `set`, `status`, `scan`, `install`, `uninstall`) wire the packages together. `install` is self-bootstrapping: it copies the binary to `/usr/local/bin`, writes a default config, writes and enables a systemd unit, and creates the NetworkManager connection profile. See `docs/superpowers/specs/2026-04-09-promised-lan-wifi-manager-design.md` for the full design.

**Tech Stack:** Go 1.22+ (stdlib `log/slog`), `github.com/urfave/cli/v2`, `gopkg.in/yaml.v3`, stdlib `net/http`, `os/exec`. Runtime dependency: `nmcli` present on the target (default on Pi OS Bookworm).

---

## File structure

Files this plan creates (in dependency order):

| Path                                      | Responsibility                                                    |
| ----------------------------------------- | ----------------------------------------------------------------- |
| `go.mod`, `go.sum`                        | Go module definition                                              |
| `cmd/promised-lan/main.go`                | `urfave/cli/v2` app wiring                                        |
| `internal/config/config.go`               | YAML config struct, defaults, validation                          |
| `internal/config/config_test.go`          | Table tests for validation and loading                            |
| `internal/netiface/bind.go`               | Resolve interface name → current IPv4                             |
| `internal/netiface/bind_test.go`          | Test against `lo` (always present)                                |
| `internal/nm/client.go`                   | `Client` interface, `Status`, `Network`, config wiring            |
| `internal/nm/runner.go`                   | `exec.Cmd` wrapper with PSK redaction in logs                     |
| `internal/nm/runner_test.go`              | Redaction + stderr capture tests                                  |
| `internal/nm/parse.go`                    | `-t -f` terse parser + per-command typed parsers                  |
| `internal/nm/parse_test.go`               | Golden-file tests                                                 |
| `internal/nm/testdata/*.txt`              | Real nmcli terse output samples                                   |
| `internal/nm/errors.go`                   | Exit-code + stderr → `ErrorKind` classification                   |
| `internal/nm/errors_test.go`              | Table tests                                                       |
| `internal/nm/nmcli.go`                    | Real `Client` implementation                                      |
| `internal/nm/nmcli_integration_test.go`   | `//go:build integration` — real nmcli on Linux                    |
| `internal/nm/fake.go`                     | In-memory `Client` test double                                    |
| `internal/web/json.go`                    | JSON request/response types                                       |
| `internal/web/handlers.go`                | HTTP handlers                                                     |
| `internal/web/handlers_test.go`           | Handler tests using `nm.Fake`                                     |
| `internal/web/server.go`                  | Interface-bound listener, lifecycle                               |
| `internal/web/server_test.go`             | Bind + shutdown tests                                             |
| `internal/web/assets/index.html`          | Embedded web UI (from `ui-preview/index.html`, wired to real API) |
| `internal/cli/serve.go`                   | `serve` subcommand                                                |
| `internal/cli/status.go`                  | `status` subcommand                                               |
| `internal/cli/scan.go`                    | `scan` subcommand                                                 |
| `internal/cli/set.go`                     | `set` subcommand                                                  |
| `internal/cli/preflight.go`               | Install preflight checks                                          |
| `internal/cli/preflight_test.go`          | Preflight tests with injected fakes                               |
| `internal/cli/install.go`                 | `install` subcommand                                              |
| `internal/cli/uninstall.go`               | `uninstall` subcommand                                            |
| `systemd/promised-lan.service`            | systemd unit template (embedded)                                  |

---

## Task list

Tasks 1–3 bootstrap the project. Tasks 4–12 build the `nm` package bottom-up (types → parser → runner → errors → fake → real impl). Tasks 13–18 build the web layer. Tasks 19–26 build the CLI subcommands. Tasks 27–29 wire everything together and add the integration test harness.

---

## Task 1: Bootstrap Go module and directory layout

**Files:**
- Create: `go.mod`
- Create: `cmd/promised-lan/main.go`
- Create: `.gitignore`

- [ ] **Step 1: Initialize git repo and Go module**

Run from the project root:
```bash
git init
go mod init github.com/savaki/promised-lan
```

- [ ] **Step 2: Create `.gitignore`**

Create `.gitignore`:
```gitignore
/promised-lan
/dist/
*.test
*.out
.DS_Store
```

- [ ] **Step 3: Create minimal `cmd/promised-lan/main.go`**

Create `cmd/promised-lan/main.go`:
```go
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "promised-lan: not implemented yet")
	os.Exit(1)
}
```

- [ ] **Step 4: Verify it builds**

Run:
```bash
go build ./cmd/promised-lan
```
Expected: produces a `promised-lan` binary in the current directory, exit 0.

- [ ] **Step 5: Commit**

```bash
git add go.mod .gitignore cmd/promised-lan/main.go
git rm -f promised-lan
git commit -m "chore: bootstrap go module"
```

---

## Task 2: `internal/config` package — types, defaults, validation, loading

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/config/config_test.go`:
```go
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
```

- [ ] **Step 2: Run test — expect compile failure**

Run:
```bash
go test ./internal/config/...
```
Expected: FAIL with "package config: no Go files".

- [ ] **Step 3: Add dependency**

Run:
```bash
go get gopkg.in/yaml.v3
```

- [ ] **Step 4: Implement `internal/config/config.go`**

Create `internal/config/config.go`:
```go
// Package config loads and validates the promised-lan YAML configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the on-disk configuration for promised-lan.
type Config struct {
	UpstreamInterface     string `yaml:"upstream_interface"`
	InteriorInterface     string `yaml:"interior_interface"`
	HTTPPort              int    `yaml:"http_port"`
	ConnectionName        string `yaml:"connection_name"`
	ConnectTimeoutSeconds int    `yaml:"connect_timeout_seconds"`
}

var connectionNameRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// applyDefaults fills zero-valued fields with the documented defaults.
func (c *Config) applyDefaults() {
	if c.UpstreamInterface == "" {
		c.UpstreamInterface = "wlan1"
	}
	if c.InteriorInterface == "" {
		c.InteriorInterface = "wlan0"
	}
	if c.HTTPPort == 0 {
		c.HTTPPort = 8080
	}
	if c.ConnectionName == "" {
		c.ConnectionName = "promised-lan-upstream"
	}
	if c.ConnectTimeoutSeconds == 0 {
		c.ConnectTimeoutSeconds = 20
	}
}

// Validate returns all validation errors joined with errors.Join.
func (c Config) Validate() error {
	var errs []error
	if c.UpstreamInterface == c.InteriorInterface {
		errs = append(errs, errors.New("upstream_interface must not equal interior_interface"))
	}
	if c.HTTPPort < 1 || c.HTTPPort > 65535 {
		errs = append(errs, fmt.Errorf("http_port must be 1..65535, got %d", c.HTTPPort))
	}
	if c.ConnectTimeoutSeconds < 5 || c.ConnectTimeoutSeconds > 120 {
		errs = append(errs, fmt.Errorf("connect_timeout_seconds must be 5..120, got %d", c.ConnectTimeoutSeconds))
	}
	if !connectionNameRe.MatchString(c.ConnectionName) {
		errs = append(errs, fmt.Errorf("connection_name %q must match %s", c.ConnectionName, connectionNameRe.String()))
	}
	return errors.Join(errs...)
}

// Load reads a YAML config file, applies defaults, and validates.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return c, nil
}

// Marshal returns the canonical YAML form of this config, suitable for writing
// as the default /etc/promised-lan/config.yaml.
func (c Config) Marshal() ([]byte, error) {
	var buf strings.Builder
	buf.WriteString("# promised-lan configuration\n")
	out, err := yaml.Marshal(c)
	if err != nil {
		return nil, err
	}
	buf.Write(out)
	return []byte(buf.String()), nil
}
```

- [ ] **Step 5: Run tests — expect pass**

Run:
```bash
go test ./internal/config/...
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/config/
git commit -m "feat(config): YAML config loading with defaults and validation"
```

---

## Task 3: `internal/netiface` package — resolve interface to IPv4

**Files:**
- Create: `internal/netiface/bind.go`
- Create: `internal/netiface/bind_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/netiface/bind_test.go`:
```go
package netiface

import (
	"errors"
	"testing"
)

func TestInterfaceIPv4_Loopback(t *testing.T) {
	// The "lo" interface always has 127.0.0.1 on Linux/macOS test hosts.
	addr, err := InterfaceIPv4("lo")
	if err != nil {
		// On some CI images "lo" might be named differently; try "lo0" (macOS).
		addr, err = InterfaceIPv4("lo0")
		if err != nil {
			t.Skipf("no loopback interface found: %v", err)
		}
	}
	if !addr.IsLoopback() {
		t.Errorf("expected loopback address, got %s", addr)
	}
}

func TestInterfaceIPv4_Missing(t *testing.T) {
	_, err := InterfaceIPv4("definitely-does-not-exist-0")
	if err == nil {
		t.Fatal("expected error for missing interface")
	}
	if !errors.Is(err, ErrNoIPv4) && !errors.Is(err, ErrNoInterface) {
		t.Errorf("expected ErrNoIPv4 or ErrNoInterface, got %v", err)
	}
}
```

- [ ] **Step 2: Run test — expect compile failure**

Run:
```bash
go test ./internal/netiface/...
```
Expected: FAIL with "package netiface: no Go files".

- [ ] **Step 3: Implement `internal/netiface/bind.go`**

Create `internal/netiface/bind.go`:
```go
// Package netiface resolves a Linux network interface name to its current IPv4.
package netiface

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
)

// ErrNoInterface is returned when the named interface does not exist.
var ErrNoInterface = errors.New("interface not found")

// ErrNoIPv4 is returned when the interface exists but has no IPv4 address.
var ErrNoIPv4 = errors.New("interface has no IPv4 address")

// InterfaceIPv4 returns the first IPv4 address assigned to the named interface.
func InterfaceIPv4(name string) (netip.Addr, error) {
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("%w: %s: %v", ErrNoInterface, name, err)
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return netip.Addr{}, fmt.Errorf("read addrs for %s: %w", name, err)
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip4 := ipnet.IP.To4()
		if ip4 == nil {
			continue
		}
		addr, ok := netip.AddrFromSlice(ip4)
		if !ok {
			continue
		}
		return addr, nil
	}
	return netip.Addr{}, fmt.Errorf("%w: %s", ErrNoIPv4, name)
}
```

- [ ] **Step 4: Run tests — expect pass**

Run:
```bash
go test ./internal/netiface/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/netiface/
git commit -m "feat(netiface): resolve interface name to IPv4 address"
```

---

## Task 4: `internal/nm` — types and `Client` interface

**Files:**
- Create: `internal/nm/client.go`

- [ ] **Step 1: Create `internal/nm/client.go`**

Create `internal/nm/client.go`:
```go
// Package nm is the only package that interacts with NetworkManager via nmcli.
// Everything else in promised-lan depends on the Client interface.
package nm

import (
	"context"
	"time"
)

// Client talks to NetworkManager for a single managed connection profile.
// All operations are scoped to the configured connection name; this package
// never enumerates or touches any other profile.
type Client interface {
	// Status returns the current state of the upstream link.
	// Returns a Status with Link=="down" (and no error) when the profile
	// exists but is not currently active. Returns an error only on system
	// or nmcli failure.
	Status(ctx context.Context) (Status, error)

	// Scan returns networks visible on the upstream radio.
	Scan(ctx context.Context) ([]Network, error)

	// UpdateAndActivate writes new credentials to the managed profile and
	// attempts to bring it up. Blocks until either activation succeeds,
	// nmcli reports failure, or the context deadline fires. The psk is
	// treated as a secret and is never logged.
	UpdateAndActivate(ctx context.Context, ssid, psk string) (Status, error)

	// EnsureProfile creates the managed connection profile if it does not
	// already exist. Idempotent.
	EnsureProfile(ctx context.Context) error
}

// Status is a snapshot of the upstream link.
type Status struct {
	Link      string    // "up" or "down"
	SSID      string    // currently associated SSID, "" if down
	State     string    // human-readable state, e.g. "associated · wpa2-psk"
	RSSI      string    // e.g. "-52 dBm", "" if down
	Signal    int       // 0..5 bars
	IPv4      string    // "192.168.1.42/24" or ""
	Gateway   string    // "192.168.1.1" or ""
	Since     time.Time // connection start time, zero if down
	Interface string    // upstream interface name
}

// Network is one entry in a scan result.
type Network struct {
	SSID    string
	Signal  int    // 0..5 bars
	Sec     string // "open", "wpa2", "wpa3"
	Current bool   // true if this SSID is what the upstream is associated to
}

// SignalBars maps an nmcli SIGNAL percentage (0..100) to our 0..5 bar scale.
func SignalBars(pct int) int {
	switch {
	case pct <= 0:
		return 0
	case pct <= 20:
		return 1
	case pct <= 40:
		return 2
	case pct <= 60:
		return 3
	case pct <= 80:
		return 4
	default:
		return 5
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run:
```bash
go build ./internal/nm/...
```
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/nm/client.go
git commit -m "feat(nm): define Client interface and core types"
```

---

## Task 5: `internal/nm` — terse output parser

**Files:**
- Create: `internal/nm/parse.go`
- Create: `internal/nm/parse_test.go`
- Create: `internal/nm/testdata/con_show.txt`
- Create: `internal/nm/testdata/dev_show.txt`
- Create: `internal/nm/testdata/dev_wifi_list.txt`

- [ ] **Step 1: Create golden files (real nmcli terse output samples)**

Create `internal/nm/testdata/con_show.txt`:
```
promised-lan-upstream:3ab8a7f2-1234-5678-aaaa-bbbbbbbbbbbb:yes:wlan1
interior-ap:d1f9c4e3-9999-8888-cccc-dddddddddddd:yes:wlan0
```

Create `internal/nm/testdata/dev_show.txt`:
```
GENERAL.STATE:100 (connected)
GENERAL.CONNECTION:promised-lan-upstream
IP4.ADDRESS[1]:192.168.1.42/24
IP4.GATEWAY:192.168.1.1
IP4.DNS[1]:192.168.1.1
```

Create `internal/nm/testdata/dev_wifi_list.txt`:
```
yes:HomeWifi:78:WPA2
no:CoffeeShop:62:--
no:Neighbor-5G:41:WPA2
no:XFINITY:28:WPA2
no:iPhone\:matt:15:WPA3
```

- [ ] **Step 2: Write failing tests**

Create `internal/nm/parse_test.go`:
```go
package nm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseTerse(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"simple", "a:b:c", []string{"a", "b", "c"}},
		{"escaped colon", `a:b\:c:d`, []string{"a", "b:c", "d"}},
		{"escaped backslash", `a\\b:c`, []string{`a\b`, "c"}},
		{"trailing escaped colon", `a:b\:`, []string{"a", "b:"}},
		{"empty fields", `::a::`, []string{"", "", "a", "", ""}},
		{"single field", `abc`, []string{"abc"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTerse(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("fields: got %d (%v), want %d (%v)", len(got), got, len(tt.want), tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("field %d: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseConShow(t *testing.T) {
	data := readGolden(t, "con_show.txt")
	profiles := parseConShow(data)
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}
	if profiles[0].Name != "promised-lan-upstream" {
		t.Errorf("got %q, want promised-lan-upstream", profiles[0].Name)
	}
	if profiles[0].Device != "wlan1" {
		t.Errorf("got %q, want wlan1", profiles[0].Device)
	}
}

func TestParseDevShow(t *testing.T) {
	data := readGolden(t, "dev_show.txt")
	ds := parseDevShow(data)
	if ds.State != "100 (connected)" {
		t.Errorf("state: %q", ds.State)
	}
	if ds.Connection != "promised-lan-upstream" {
		t.Errorf("conn: %q", ds.Connection)
	}
	if ds.IPv4 != "192.168.1.42/24" {
		t.Errorf("ipv4: %q", ds.IPv4)
	}
	if ds.Gateway != "192.168.1.1" {
		t.Errorf("gw: %q", ds.Gateway)
	}
}

func TestParseDevWifiList(t *testing.T) {
	data := readGolden(t, "dev_wifi_list.txt")
	entries := parseDevWifiList(data)
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}
	if !entries[0].Current {
		t.Error("first entry should be current")
	}
	if entries[0].SSID != "HomeWifi" {
		t.Errorf("ssid: %q", entries[0].SSID)
	}
	if entries[0].Signal != 4 {
		t.Errorf("signal bars: got %d, want 4 (for 78%%)", entries[0].Signal)
	}
	if entries[0].Sec != "wpa2" {
		t.Errorf("sec: %q", entries[0].Sec)
	}
	if entries[1].Sec != "open" {
		t.Errorf("coffee shop sec: %q, want open", entries[1].Sec)
	}
	if entries[4].SSID != "iPhone:matt" {
		t.Errorf("escaped ssid: %q", entries[4].SSID)
	}
}

func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
```

- [ ] **Step 3: Run tests — expect compile failure**

Run:
```bash
go test ./internal/nm/...
```
Expected: FAIL (undefined parseTerse, etc.).

- [ ] **Step 4: Implement `internal/nm/parse.go`**

Create `internal/nm/parse.go`:
```go
package nm

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
)

// parseTerse splits one line of `nmcli -t` output into fields. nmcli escapes
// literal ':' as `\:` and literal '\' as `\\`, and we unescape them here.
func parseTerse(line string) []string {
	var fields []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '\\' && i+1 < len(line) {
			next := line[i+1]
			if next == ':' || next == '\\' {
				cur.WriteByte(next)
				i++
				continue
			}
		}
		if c == ':' {
			fields = append(fields, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(c)
	}
	fields = append(fields, cur.String())
	return fields
}

// connectionProfile is the typed form of one `con show` row.
type connectionProfile struct {
	Name   string
	UUID   string
	Active string
	Device string
}

// parseConShow parses `nmcli -t -f NAME,UUID,ACTIVE,DEVICE con show [--active]`.
func parseConShow(data []byte) []connectionProfile {
	var out []connectionProfile
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		f := parseTerse(line)
		p := connectionProfile{}
		if len(f) > 0 {
			p.Name = f[0]
		}
		if len(f) > 1 {
			p.UUID = f[1]
		}
		if len(f) > 2 {
			p.Active = f[2]
		}
		if len(f) > 3 {
			p.Device = f[3]
		}
		out = append(out, p)
	}
	return out
}

// devShow is the typed form of `nmcli -t -f GENERAL.STATE,IP4.ADDRESS,IP4.GATEWAY,GENERAL.CONNECTION dev show <iface>`.
//
// IP4.ADDRESS in terse mode can be multi-valued as IP4.ADDRESS[1], IP4.ADDRESS[2], ...
// We take the first.
type devShow struct {
	State      string
	IPv4       string
	Gateway    string
	Connection string
}

func parseDevShow(data []byte) devShow {
	var ds devShow
	seenIPv4 := false
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		f := parseTerse(line)
		if len(f) < 2 {
			continue
		}
		key, val := f[0], f[1]
		switch {
		case key == "GENERAL.STATE":
			ds.State = val
		case key == "GENERAL.CONNECTION":
			ds.Connection = val
		case strings.HasPrefix(key, "IP4.ADDRESS") && !seenIPv4:
			ds.IPv4 = val
			seenIPv4 = true
		case key == "IP4.GATEWAY":
			ds.Gateway = val
		}
	}
	return ds
}

// parseDevWifiList parses `nmcli -t -f ACTIVE,SSID,SIGNAL,SECURITY dev wifi list`.
// Dedupes by SSID keeping the highest-signal row. Filters empty SSIDs.
func parseDevWifiList(data []byte) []Network {
	byName := make(map[string]Network)
	var order []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		f := parseTerse(line)
		if len(f) < 4 {
			continue
		}
		active, ssid, sigStr, sec := f[0], f[1], f[2], f[3]
		if ssid == "" {
			continue
		}
		sig, _ := strconv.Atoi(sigStr)
		n := Network{
			SSID:    ssid,
			Signal:  SignalBars(sig),
			Sec:     normalizeSec(sec),
			Current: active == "yes",
		}
		existing, ok := byName[ssid]
		if !ok {
			byName[ssid] = n
			order = append(order, ssid)
			continue
		}
		if n.Signal > existing.Signal {
			// Preserve Current flag: if either row claims current, entry is current.
			n.Current = n.Current || existing.Current
			byName[ssid] = n
		} else if n.Current {
			existing.Current = true
			byName[ssid] = existing
		}
	}
	out := make([]Network, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}
	return out
}

func normalizeSec(s string) string {
	s = strings.TrimSpace(s)
	switch {
	case s == "" || s == "--":
		return "open"
	case strings.Contains(s, "WPA3"):
		return "wpa3"
	case strings.Contains(s, "WPA2"), strings.Contains(s, "WPA1"), strings.Contains(s, "WPA"):
		return "wpa2"
	default:
		return strings.ToLower(s)
	}
}
```

- [ ] **Step 5: Run tests — expect pass**

Run:
```bash
go test ./internal/nm/...
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/nm/parse.go internal/nm/parse_test.go internal/nm/testdata/
git commit -m "feat(nm): terse output parser with golden file tests"
```

---

## Task 6: `internal/nm` — error classification

**Files:**
- Create: `internal/nm/errors.go`
- Create: `internal/nm/errors_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/nm/errors_test.go`:
```go
package nm

import (
	"errors"
	"testing"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name     string
		exitCode int
		stderr   string
		want     ErrorKind
	}{
		{"timeout exit 3", 3, "Timeout expired (30 seconds)", ErrTimeout},
		{"auth rejected", 4, "Error: Connection activation failed: (7) Secrets were required, but not provided.", ErrAuthRejected},
		{"no secrets", 4, "Error: Connection activation failed: no secrets", ErrNoSecrets},
		{"dhcp expired", 4, "Error: Connection activation failed: (32) ip-config-expired.", ErrDHCPTimeout},
		{"other activation failure", 4, "Error: Connection activation failed: unknown reason", ErrUnknown},
		{"nm down", 8, "Error: NetworkManager is not running.", ErrNMDown},
		{"does not exist", 10, "Error: unknown connection 'foo'.", ErrNetworkNotFound},
		{"unknown exit", 99, "mystery", ErrUnknown},
		{"success", 0, "", ErrNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.exitCode, tt.stderr)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestErrorKindError(t *testing.T) {
	err := &NMError{Kind: ErrAuthRejected, Stderr: "foo"}
	if !errors.Is(err, ErrAuthRejected) {
		t.Error("errors.Is must match ErrorKind sentinel")
	}
	if err.Error() == "" {
		t.Error("Error() must not be empty")
	}
}
```

- [ ] **Step 2: Run test — expect compile failure**

Run:
```bash
go test ./internal/nm/...
```
Expected: FAIL.

- [ ] **Step 3: Implement `internal/nm/errors.go`**

Create `internal/nm/errors.go`:
```go
package nm

import (
	"fmt"
	"strings"
)

// ErrorKind is a classification of nmcli failure modes based on exit code and stderr.
type ErrorKind int

const (
	ErrNone            ErrorKind = iota // success (exit 0)
	ErrUnknown                          // anything we don't recognize
	ErrTimeout                          // -w deadline expired (exit 3)
	ErrAuthRejected                     // 4-way handshake failure (exit 4, secrets required)
	ErrDHCPTimeout                      // associated but no IP lease (exit 4, ip-config-expired)
	ErrNoSecrets                        // no secret agent provided psk (exit 4, no secrets)
	ErrNMDown                           // NetworkManager not running (exit 8)
	ErrNetworkNotFound                  // connection/device/AP does not exist (exit 10)
	ErrInterfaceDown                    // upstream interface rfkilled or missing
)

// Error implements the error interface so ErrorKind values can be used directly
// or via errors.Is with NMError.
func (k ErrorKind) Error() string {
	switch k {
	case ErrNone:
		return "success"
	case ErrTimeout:
		return "timed out waiting for association"
	case ErrAuthRejected:
		return "authentication rejected by access point"
	case ErrDHCPTimeout:
		return "associated but no DHCP lease"
	case ErrNoSecrets:
		return "no passphrase available"
	case ErrNMDown:
		return "NetworkManager is not running"
	case ErrNetworkNotFound:
		return "network not found"
	case ErrInterfaceDown:
		return "upstream interface is down"
	default:
		return "unknown error"
	}
}

// NMError is the error type returned from Client operations.
type NMError struct {
	Kind   ErrorKind
	Stderr string
}

func (e *NMError) Error() string {
	if e.Stderr == "" {
		return e.Kind.Error()
	}
	return fmt.Sprintf("%s: %s", e.Kind.Error(), strings.TrimSpace(e.Stderr))
}

func (e *NMError) Unwrap() error { return e.Kind }

// Classify maps an nmcli exit code and stderr to an ErrorKind.
func Classify(exitCode int, stderr string) ErrorKind {
	if exitCode == 0 {
		return ErrNone
	}
	s := strings.ToLower(stderr)
	switch exitCode {
	case 3:
		return ErrTimeout
	case 4:
		switch {
		case strings.Contains(s, "ip-config-expired"):
			return ErrDHCPTimeout
		case strings.Contains(s, "no secrets"):
			return ErrNoSecrets
		case strings.Contains(s, "secrets were required"), strings.Contains(s, "4-way handshake"):
			return ErrAuthRejected
		default:
			return ErrUnknown
		}
	case 8:
		return ErrNMDown
	case 10:
		return ErrNetworkNotFound
	default:
		return ErrUnknown
	}
}
```

- [ ] **Step 4: Run tests — expect pass**

Run:
```bash
go test ./internal/nm/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/nm/errors.go internal/nm/errors_test.go
git commit -m "feat(nm): classify nmcli exit codes and stderr into error kinds"
```

---

## Task 7: `internal/nm` — exec runner with PSK redaction

**Files:**
- Create: `internal/nm/runner.go`
- Create: `internal/nm/runner_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/nm/runner_test.go`:
```go
package nm

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestRunnerRedactsPSK(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := newRunner(logger)
	// Use /bin/echo as a fake nmcli so the test runs on any host.
	r.binary = "/bin/echo"
	args := []string{"con", "mod", "foo", "wifi-sec.psk", "topsecret"}
	out, err := r.run(context.Background(), args, 4) // psk at index 4
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// Stdout from echo just mirrors args — that's fine, we're testing logs.
	if !bytes.Contains(out, []byte("topsecret")) {
		t.Errorf("echo output missing: %s", out)
	}
	log := logBuf.String()
	if strings.Contains(log, "topsecret") {
		t.Errorf("psk leaked into log: %s", log)
	}
	if !strings.Contains(log, "***") {
		t.Errorf("redaction marker missing from log: %s", log)
	}
}

func TestRunnerFailure(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	r := newRunner(logger)
	r.binary = "/bin/sh"
	// Exit 42 via sh -c "exit 42"
	_, err := r.run(context.Background(), []string{"-c", "exit 42"})
	if err == nil {
		t.Fatal("expected error from exit 42")
	}
	var nerr *NMError
	if !asNMError(err, &nerr) {
		t.Fatalf("expected *NMError, got %T: %v", err, err)
	}
	if nerr.Kind != ErrUnknown {
		t.Errorf("kind: got %v, want ErrUnknown for exit 42", nerr.Kind)
	}
}

// asNMError is a local helper replacing errors.As for tests.
func asNMError(err error, target **NMError) bool {
	for err != nil {
		if e, ok := err.(*NMError); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		if u, ok := err.(unwrapper); ok {
			err = u.Unwrap()
		} else {
			break
		}
	}
	return false
}
```

- [ ] **Step 2: Run test — expect compile failure**

Run:
```bash
go test ./internal/nm/...
```
Expected: FAIL.

- [ ] **Step 3: Implement `internal/nm/runner.go`**

Create `internal/nm/runner.go`:
```go
package nm

import (
	"bytes"
	"context"
	"log/slog"
	"os/exec"
)

// runner is a small wrapper around os/exec that redacts sensitive args in logs
// and converts nmcli exit codes to typed NMError values.
type runner struct {
	binary string
	logger *slog.Logger
}

func newRunner(logger *slog.Logger) *runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &runner{binary: "nmcli", logger: logger}
}

// run executes r.binary with args. redactIdx lists argv positions whose values
// should be replaced with "***" in any logged form. Returns stdout on success
// or an *NMError on failure.
func (r *runner) run(ctx context.Context, args []string, redactIdx ...int) ([]byte, error) {
	logArgs := make([]string, len(args))
	copy(logArgs, args)
	for _, i := range redactIdx {
		if i >= 0 && i < len(logArgs) {
			logArgs[i] = "***"
		}
	}
	r.logger.Debug("exec", "bin", r.binary, "args", logArgs)

	cmd := exec.CommandContext(ctx, r.binary, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		exitCode := -1
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		}
		kind := Classify(exitCode, stderr.String())
		if kind == ErrNone {
			kind = ErrUnknown
		}
		r.logger.Debug("exec failed",
			"bin", r.binary,
			"args", logArgs,
			"exit", exitCode,
			"stderr", stderr.String(),
			"kind", kind)
		return out, &NMError{Kind: kind, Stderr: stderr.String()}
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests — expect pass**

Run:
```bash
go test ./internal/nm/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/nm/runner.go internal/nm/runner_test.go
git commit -m "feat(nm): exec runner with PSK redaction and typed errors"
```

---

## Task 8: `internal/nm` — in-memory `Fake` implementation

**Files:**
- Create: `internal/nm/fake.go`
- Create: `internal/nm/fake_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/nm/fake_test.go`:
```go
package nm

import (
	"context"
	"testing"
	"time"
)

// Compile-time check: *Fake must satisfy Client.
var _ Client = (*Fake)(nil)

func TestFakeStatusAndUpdate(t *testing.T) {
	f := NewFake()
	f.SetStatus(Status{Link: "down"})

	ctx := context.Background()
	st, err := f.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Link != "down" {
		t.Errorf("link: %q", st.Link)
	}

	// Configure fake to accept the next update as a success.
	f.SetNextUpdateResult(Status{
		Link:      "up",
		SSID:      "NewNet",
		IPv4:      "10.0.0.2/24",
		Signal:    5,
		Interface: "wlan1",
		Since:     time.Now(),
	}, nil)

	st2, err := f.UpdateAndActivate(ctx, "NewNet", "secretpsk")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if st2.SSID != "NewNet" {
		t.Errorf("ssid: %q", st2.SSID)
	}

	// After update, Status should return the new state.
	st3, _ := f.Status(ctx)
	if st3.SSID != "NewNet" {
		t.Errorf("status after update: %q", st3.SSID)
	}

	// Fake must not record the psk in plaintext anywhere observable.
	if f.LastPSK != "" {
		t.Errorf("fake retained psk: %q", f.LastPSK)
	}
}

func TestFakeUpdateFailure(t *testing.T) {
	f := NewFake()
	f.SetStatus(Status{Link: "up", SSID: "Old"})
	f.SetNextUpdateResult(Status{Link: "down", SSID: "Old"}, &NMError{Kind: ErrAuthRejected})

	_, err := f.UpdateAndActivate(context.Background(), "Bad", "badpass1")
	if err == nil {
		t.Fatal("expected error")
	}

	// On failure the stored status must match what the fake was told to return.
	st, _ := f.Status(context.Background())
	if st.Link != "down" {
		t.Errorf("expected link down after failed update, got %q", st.Link)
	}
}

func TestFakeScan(t *testing.T) {
	f := NewFake()
	f.SetScan([]Network{
		{SSID: "Alpha", Signal: 5, Sec: "wpa2", Current: true},
		{SSID: "Beta", Signal: 3, Sec: "open"},
	})
	nets, err := f.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(nets) != 2 {
		t.Fatalf("expected 2 networks, got %d", len(nets))
	}
}
```

- [ ] **Step 2: Run test — expect compile failure**

Run:
```bash
go test ./internal/nm/...
```
Expected: FAIL.

- [ ] **Step 3: Implement `internal/nm/fake.go`**

Create `internal/nm/fake.go`:
```go
package nm

import (
	"context"
	"sync"
)

// Fake is an in-memory Client suitable for tests. It never stores PSKs: the
// UpdateAndActivate method immediately drops its psk argument after using it
// to decide whether the staged result applies.
type Fake struct {
	mu     sync.Mutex
	status Status
	scan   []Network
	next   *fakeUpdateResult

	// LastPSK is intentionally not stored; kept as an empty field so tests
	// can assert it remains empty after calls.
	LastPSK string
}

type fakeUpdateResult struct {
	Status Status
	Err    error
}

func NewFake() *Fake {
	return &Fake{status: Status{Link: "down"}}
}

func (f *Fake) SetStatus(s Status) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = s
}

func (f *Fake) SetScan(n []Network) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scan = append([]Network(nil), n...)
}

// SetNextUpdateResult stages the result of the next UpdateAndActivate call.
// If err is nil, the staged Status becomes the new current status on success.
// If err is non-nil, the staged Status is still set (reflecting observed state
// after the failed attempt) and the error is returned.
func (f *Fake) SetNextUpdateResult(s Status, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next = &fakeUpdateResult{Status: s, Err: err}
}

func (f *Fake) Status(ctx context.Context) (Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, nil
}

func (f *Fake) Scan(ctx context.Context) ([]Network, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Network(nil), f.scan...), nil
}

func (f *Fake) UpdateAndActivate(ctx context.Context, ssid, psk string) (Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_ = psk // never stored
	if f.next == nil {
		f.status = Status{Link: "up", SSID: ssid, Interface: f.status.Interface}
		return f.status, nil
	}
	res := f.next
	f.next = nil
	f.status = res.Status
	return res.Status, res.Err
}

func (f *Fake) EnsureProfile(ctx context.Context) error {
	return nil
}
```

- [ ] **Step 4: Run tests — expect pass**

Run:
```bash
go test ./internal/nm/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/nm/fake.go internal/nm/fake_test.go
git commit -m "feat(nm): in-memory Fake Client for tests"
```

---

## Task 9: `internal/nm` — real `nmcli.go` Client implementation

**Files:**
- Create: `internal/nm/nmcli.go`

No unit tests in this task — the real client is exercised by the integration test (Task 29). Its behavior under fake conditions is covered by the parser, runner, and error tests already in place.

- [ ] **Step 1: Implement `internal/nm/nmcli.go`**

Create `internal/nm/nmcli.go`:
```go
package nm

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// NMCLI is the production Client. It shells out to nmcli through a runner.
type NMCLI struct {
	runner            *runner
	upstreamInterface string
	connectionName    string
	connectTimeout    time.Duration
	logger            *slog.Logger
}

// NMCLIConfig configures a new NMCLI client.
type NMCLIConfig struct {
	UpstreamInterface string
	ConnectionName    string
	ConnectTimeout    time.Duration
	Logger            *slog.Logger
}

// NewNMCLI constructs a new production Client.
func NewNMCLI(cfg NMCLIConfig) *NMCLI {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &NMCLI{
		runner:            newRunner(logger),
		upstreamInterface: cfg.UpstreamInterface,
		connectionName:    cfg.ConnectionName,
		connectTimeout:    cfg.ConnectTimeout,
		logger:            logger,
	}
}

var _ Client = (*NMCLI)(nil)

func (n *NMCLI) EnsureProfile(ctx context.Context) error {
	// List profile names; skip creation if ours exists.
	out, err := n.runner.run(ctx, []string{"-t", "-f", "NAME", "con", "show"})
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == n.connectionName {
			return nil
		}
	}
	// Create with placeholder ssid + placeholder psk (so it is never a
	// transient insecure profile on disk).
	args := []string{
		"con", "add",
		"type", "wifi",
		"ifname", n.upstreamInterface,
		"con-name", n.connectionName,
		"ssid", "placeholder",
		"wifi-sec.key-mgmt", "wpa-psk",
		"wifi-sec.psk", "placeholder-psk",
		"connection.autoconnect", "yes",
		"connection.interface-name", n.upstreamInterface,
	}
	// Redact placeholder psk value at index 13 for consistency.
	_, err = n.runner.run(ctx, args, 13)
	return err
}

func (n *NMCLI) Status(ctx context.Context) (Status, error) {
	st := Status{Interface: n.upstreamInterface, Link: "down"}

	// Device-level status (state, ip, gateway, active connection name).
	devOut, err := n.runner.run(ctx, []string{
		"-t",
		"-f", "GENERAL.STATE,IP4.ADDRESS,IP4.GATEWAY,GENERAL.CONNECTION",
		"dev", "show", n.upstreamInterface,
	})
	if err != nil {
		return st, err
	}
	dev := parseDevShow(devOut)
	st.IPv4 = dev.IPv4
	st.Gateway = dev.Gateway

	// If this device isn't running our profile, call it down.
	if dev.Connection != n.connectionName {
		return st, nil
	}

	// Wifi-level data (current SSID + signal).
	wifiOut, err := n.runner.run(ctx, []string{
		"-t",
		"-f", "ACTIVE,SSID,SIGNAL,SECURITY",
		"dev", "wifi", "list", "ifname", n.upstreamInterface,
	})
	if err != nil {
		return st, err
	}
	wifi := parseDevWifiList(wifiOut)
	for _, w := range wifi {
		if w.Current {
			st.SSID = w.SSID
			st.Signal = w.Signal
			st.State = fmt.Sprintf("associated · %s", w.Sec)
			break
		}
	}

	if st.SSID != "" && st.IPv4 != "" {
		st.Link = "up"
	}
	// RSSI: nmcli gives SIGNAL as a percentage, not dBm. We report the bar
	// count in st.Signal and synthesize a human-readable string here.
	if st.Signal > 0 {
		st.RSSI = fmt.Sprintf("%d/5", st.Signal)
	}
	return st, nil
}

func (n *NMCLI) Scan(ctx context.Context) ([]Network, error) {
	// --rescan yes triggers a rescan if rate limits allow, otherwise returns
	// cached results. Either is acceptable for the UI's scan button.
	out, err := n.runner.run(ctx, []string{
		"-t",
		"-f", "ACTIVE,SSID,SIGNAL,SECURITY",
		"dev", "wifi", "list",
		"ifname", n.upstreamInterface,
		"--rescan", "yes",
	})
	if err != nil {
		return nil, err
	}
	return parseDevWifiList(out), nil
}

func (n *NMCLI) UpdateAndActivate(ctx context.Context, ssid, psk string) (Status, error) {
	// Ensure the profile exists (idempotent).
	if err := n.EnsureProfile(ctx); err != nil {
		return Status{Link: "down", Interface: n.upstreamInterface}, err
	}

	// Apply new credentials. PSK is at argv index 7.
	modArgs := []string{
		"con", "mod", n.connectionName,
		"802-11-wireless.ssid", ssid,
		"wifi-sec.key-mgmt", "wpa-psk",
		"wifi-sec.psk", psk,
	}
	if _, err := n.runner.run(ctx, modArgs, 7); err != nil {
		return Status{Link: "down", Interface: n.upstreamInterface}, err
	}

	// Bring it up with a bounded wait.
	timeout := int(n.connectTimeout.Seconds())
	if timeout <= 0 {
		timeout = 20
	}
	upArgs := []string{"-w", strconv.Itoa(timeout), "con", "up", n.connectionName}
	if _, err := n.runner.run(ctx, upArgs); err != nil {
		// Even on failure, return the real status so callers can show the UI
		// what actually happened.
		st, _ := n.Status(ctx)
		return st, err
	}

	// Defensive: verify the link is actually up with an IP (the manpage does
	// not strictly guarantee con up exit 0 implies IP is configured).
	st, err := n.Status(ctx)
	if err != nil {
		return st, err
	}
	if st.Link != "up" {
		return st, &NMError{Kind: ErrUnknown, Stderr: "con up returned success but link is not up"}
	}
	return st, nil
}
```

- [ ] **Step 2: Verify it builds**

Run:
```bash
go build ./internal/nm/...
```
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/nm/nmcli.go
git commit -m "feat(nm): real nmcli-backed Client implementation"
```

---

## Task 10: `internal/web` — JSON types

**Files:**
- Create: `internal/web/json.go`

- [ ] **Step 1: Create `internal/web/json.go`**

Create `internal/web/json.go`:
```go
// Package web serves the promised-lan web UI and JSON API, bound only to the
// interior network interface.
package web

import "github.com/savaki/promised-lan/internal/nm"

// StatusResp is the GET /status response body.
type StatusResp struct {
	Link      string `json:"link"`
	SSID      string `json:"ssid"`
	State     string `json:"state"`
	RSSI      string `json:"rssi"`
	Signal    int    `json:"signal"`
	IPv4      string `json:"ipv4"`
	Gateway   string `json:"gateway"`
	Since     string `json:"since"`
	Interface string `json:"interface"`
}

func newStatusResp(s nm.Status) StatusResp {
	since := ""
	if !s.Since.IsZero() {
		since = s.Since.UTC().Format("2006-01-02T15:04:05Z")
	}
	return StatusResp{
		Link:      s.Link,
		SSID:      s.SSID,
		State:     s.State,
		RSSI:      s.RSSI,
		Signal:    s.Signal,
		IPv4:      s.IPv4,
		Gateway:   s.Gateway,
		Since:     since,
		Interface: s.Interface,
	}
}

// ScanEntry is one network in a scan response.
type ScanEntry struct {
	SSID    string `json:"ssid"`
	Signal  int    `json:"signal"`
	Sec     string `json:"sec"`
	Current bool   `json:"current"`
}

// ScanResp is the GET /scan response body.
type ScanResp struct {
	Networks []ScanEntry `json:"networks"`
}

func newScanResp(nets []nm.Network) ScanResp {
	entries := make([]ScanEntry, 0, len(nets))
	for _, n := range nets {
		entries = append(entries, ScanEntry{
			SSID:    n.SSID,
			Signal:  n.Signal,
			Sec:     n.Sec,
			Current: n.Current,
		})
	}
	return ScanResp{Networks: entries}
}

// CredsReq is the POST /credentials request body.
type CredsReq struct {
	SSID string `json:"ssid"`
	PSK  string `json:"psk"`
}

// CredsResp is the POST /credentials response body.
type CredsResp struct {
	OK      bool       `json:"ok"`
	Status  StatusResp `json:"status"`
	Message string     `json:"message"`
}

// ErrResp is a plain error response for 4xx/5xx cases.
type ErrResp struct {
	Error string `json:"error"`
}
```

- [ ] **Step 2: Verify it builds**

Run:
```bash
go build ./internal/web/...
```
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/web/json.go
git commit -m "feat(web): JSON request/response types"
```

---

## Task 11: `internal/web` — handlers

**Files:**
- Create: `internal/web/handlers.go`
- Create: `internal/web/handlers_test.go`
- Create: `internal/web/assets/index.html` (placeholder — real file lands in Task 13)

- [ ] **Step 1: Create placeholder index.html for the embed**

Create `internal/web/assets/index.html`:
```html
<!doctype html><html><body>placeholder</body></html>
```

- [ ] **Step 2: Write failing tests**

Create `internal/web/handlers_test.go`:
```go
package web

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/savaki/promised-lan/internal/nm"
)

func newTestHandler(t *testing.T, fake *nm.Fake) http.Handler {
	t.Helper()
	return NewHandler(fake, slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)))
}

func TestHandleStatus(t *testing.T) {
	f := nm.NewFake()
	f.SetStatus(nm.Status{Link: "up", SSID: "Test", IPv4: "10.0.0.5/24", Interface: "wlan1"})
	h := newTestHandler(t, f)
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status: %d, body: %s", rec.Code, rec.Body.String())
	}
	var resp StatusResp
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Link != "up" || resp.SSID != "Test" {
		t.Errorf("resp: %+v", resp)
	}
}

func TestHandleScan(t *testing.T) {
	f := nm.NewFake()
	f.SetScan([]nm.Network{
		{SSID: "A", Signal: 5, Sec: "wpa2", Current: true},
		{SSID: "B", Signal: 3, Sec: "open"},
	})
	h := newTestHandler(t, f)
	req := httptest.NewRequest(http.MethodGet, "/scan", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status: %d", rec.Code)
	}
	var resp ScanResp
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Networks) != 2 {
		t.Errorf("networks: %d", len(resp.Networks))
	}
}

func TestHandleCredentialsSuccess(t *testing.T) {
	f := nm.NewFake()
	f.SetNextUpdateResult(nm.Status{Link: "up", SSID: "NewNet", IPv4: "10.0.0.9/24"}, nil)
	h := newTestHandler(t, f)
	body := strings.NewReader(`{"ssid":"NewNet","psk":"goodpass"}`)
	req := httptest.NewRequest(http.MethodPost, "/credentials", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status: %d, body: %s", rec.Code, rec.Body.String())
	}
	var resp CredsResp
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Errorf("OK false: %+v", resp)
	}
	if resp.Status.SSID != "NewNet" {
		t.Errorf("status ssid: %q", resp.Status.SSID)
	}
}

func TestHandleCredentialsFailure(t *testing.T) {
	f := nm.NewFake()
	f.SetNextUpdateResult(nm.Status{Link: "down"}, &nm.NMError{Kind: nm.ErrAuthRejected})
	h := newTestHandler(t, f)
	body := strings.NewReader(`{"ssid":"NewNet","psk":"badpass12"}`)
	req := httptest.NewRequest(http.MethodPost, "/credentials", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status: %d", rec.Code)
	}
	var resp CredsResp
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.OK {
		t.Errorf("expected OK=false")
	}
	if !strings.Contains(resp.Message, "authentication rejected") {
		t.Errorf("message: %q", resp.Message)
	}
}

func TestHandleCredentialsValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
		code int
	}{
		{"empty ssid", `{"ssid":"","psk":"goodpass"}`, 400},
		{"short psk", `{"ssid":"X","psk":"short"}`, 400},
		{"too long ssid", `{"ssid":"` + strings.Repeat("x", 33) + `","psk":"goodpass"}`, 400},
		{"invalid json", `{not json`, 400},
	}
	f := nm.NewFake()
	h := newTestHandler(t, f)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/credentials", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.code {
				t.Errorf("got %d, want %d", rec.Code, tt.code)
			}
		})
	}
}

func TestHandleCredentialsWrongContentType(t *testing.T) {
	f := nm.NewFake()
	h := newTestHandler(t, f)
	req := httptest.NewRequest(http.MethodPost, "/credentials", strings.NewReader(`{"ssid":"X","psk":"goodpass"}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 415 {
		t.Errorf("got %d, want 415", rec.Code)
	}
}

func TestHandleIndex(t *testing.T) {
	f := nm.NewFake()
	h := newTestHandler(t, f)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "placeholder") && !strings.Contains(rec.Body.String(), "promised-lan") {
		t.Errorf("body missing expected content: %s", rec.Body.String())
	}
}
```

Tell the test file that it compiles against an interface; we'll add the handler next.

Also, import `context` won't be needed if you use `req.Context()` directly. The above compiles.

- [ ] **Step 3: Run tests — expect compile failure**

Run:
```bash
go test ./internal/web/...
```
Expected: FAIL (undefined NewHandler).

- [ ] **Step 4: Implement `internal/web/handlers.go`**

Create `internal/web/handlers.go`:
```go
package web

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/savaki/promised-lan/internal/nm"
)

//go:embed assets/index.html
var assetFS embed.FS

// NewHandler builds the promised-lan http.Handler wired to the given nm.Client.
func NewHandler(client nm.Client, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &handler{client: client, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", h.status)
	mux.HandleFunc("GET /scan", h.scan)
	mux.HandleFunc("POST /credentials", h.credentials)
	mux.HandleFunc("GET /", h.index)
	return mux
}

type handler struct {
	client nm.Client
	logger *slog.Logger
}

func (h *handler) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	sub, err := fs.Sub(assetFS, "assets")
	if err != nil {
		writeJSONError(w, 500, "asset fs")
		return
	}
	data, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		writeJSONError(w, 500, "asset read")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}

func (h *handler) status(w http.ResponseWriter, r *http.Request) {
	st, err := h.client.Status(r.Context())
	if err != nil {
		h.logger.Error("status failed", "err", err)
		writeJSONError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, newStatusResp(st))
}

func (h *handler) scan(w http.ResponseWriter, r *http.Request) {
	nets, err := h.client.Scan(r.Context())
	if err != nil {
		h.logger.Error("scan failed", "err", err)
		writeJSONError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, newScanResp(nets))
}

func (h *handler) credentials(w http.ResponseWriter, r *http.Request) {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeJSONError(w, 415, "content-type must be application/json")
		return
	}
	var req CredsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, 400, "invalid json")
		return
	}
	if l := len(req.SSID); l < 1 || l > 32 {
		writeJSONError(w, 400, "ssid length must be 1..32")
		return
	}
	if l := len(req.PSK); l < 8 || l > 63 {
		writeJSONError(w, 400, "psk length must be 8..63")
		return
	}

	st, err := h.client.UpdateAndActivate(r.Context(), req.SSID, req.PSK)
	// Do NOT log req.PSK — even on error.
	resp := CredsResp{Status: newStatusResp(st)}
	if err == nil {
		resp.OK = true
		resp.Message = "connected"
		writeJSON(w, 200, resp)
		return
	}

	var nerr *nm.NMError
	if errors.As(err, &nerr) {
		resp.Message = nerr.Kind.Error()
	} else {
		resp.Message = err.Error()
	}
	h.logger.Warn("credentials update failed", "kind", fmt.Sprintf("%v", nerr), "ssid", req.SSID)
	writeJSON(w, 200, resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrResp{Error: msg})
}
```

- [ ] **Step 5: Run tests — expect pass**

Run:
```bash
go test ./internal/web/...
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/handlers.go internal/web/handlers_test.go internal/web/assets/
git commit -m "feat(web): HTTP handlers with nm.Client wiring"
```

---

## Task 12: `internal/web` — interface-bound server lifecycle

**Files:**
- Create: `internal/web/server.go`
- Create: `internal/web/server_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/web/server_test.go`:
```go
package web

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/savaki/promised-lan/internal/config"
	"github.com/savaki/promised-lan/internal/nm"
)

func TestServerBindsToLoopback(t *testing.T) {
	f := nm.NewFake()
	f.SetStatus(nm.Status{Link: "down"})

	ifaceName := loopbackInterface(t)

	cfg := config.Config{
		InteriorInterface: ifaceName,
		HTTPPort:          0, // pick any free port
	}
	srv := NewServer(cfg, f, slog.Default())

	// Use a fixed free port for the test.
	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	srv.cfg.HTTPPort = port

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(ctx) }()

	// Wait for the server to be listening.
	url := fmt.Sprintf("http://127.0.0.1:%d/status", port)
	deadline := time.Now().Add(3 * time.Second)
	var resp *http.Response
	for time.Now().Before(deadline) {
		resp, err = http.Get(url)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d, body %s", resp.StatusCode, body)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled && err != http.ErrServerClosed {
			t.Errorf("Run returned: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("server did not shut down within 3s")
	}
}

func loopbackInterface(t *testing.T) string {
	t.Helper()
	// Try "lo" (Linux) then "lo0" (macOS).
	for _, name := range []string{"lo", "lo0"} {
		if _, err := net.InterfaceByName(name); err == nil {
			return name
		}
	}
	t.Skip("no loopback interface available")
	return ""
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
```

Add imports to the test file's top:
```go
import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
	...
)
```

- [ ] **Step 2: Run test — expect compile failure**

Run:
```bash
go test ./internal/web/...
```
Expected: FAIL.

- [ ] **Step 3: Implement `internal/web/server.go`**

Create `internal/web/server.go`:
```go
package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/savaki/promised-lan/internal/config"
	"github.com/savaki/promised-lan/internal/netiface"
	"github.com/savaki/promised-lan/internal/nm"
)

// Server is the interior-bound HTTP server.
type Server struct {
	cfg    config.Config
	client nm.Client
	logger *slog.Logger
}

func NewServer(cfg config.Config, client nm.Client, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{cfg: cfg, client: client, logger: logger}
}

// Run blocks until ctx is canceled or a fatal error occurs.
// It polls the interior interface for an IPv4 (up to 60 seconds) and then
// binds the HTTP server to exactly that address — never 0.0.0.0.
func (s *Server) Run(ctx context.Context) error {
	addr, err := s.waitForInteriorIP(ctx, 60*time.Second)
	if err != nil {
		return err
	}

	listenAddr := net.JoinHostPort(addr, strconv.Itoa(s.cfg.HTTPPort))
	ln, err := net.Listen("tcp4", listenAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", listenAddr, err)
	}

	srv := &http.Server{
		Handler:           NewHandler(s.client, s.logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	s.logger.Info("web server listening",
		"addr", listenAddr,
		"interface", s.cfg.InteriorInterface)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

func (s *Server) waitForInteriorIP(ctx context.Context, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		addr, err := netiface.InterfaceIPv4(s.cfg.InteriorInterface)
		if err == nil {
			return addr.String(), nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("interior interface %s had no IPv4 after %s: %w",
				s.cfg.InteriorInterface, timeout, err)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}
```

- [ ] **Step 4: Run tests — expect pass**

Run:
```bash
go test ./internal/web/...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web/server.go internal/web/server_test.go
git commit -m "feat(web): interface-bound HTTP server with lifecycle management"
```

---

## Task 13: Ship the real web UI from the preview

**Files:**
- Modify: `internal/web/assets/index.html` (replace placeholder)

The `ui-preview/index.html` file in the repo already contains the full UI with mocked data. We'll copy it and replace the mock-data section with real `fetch` calls.

- [ ] **Step 1: Read the existing preview file**

Run:
```bash
wc -l ui-preview/index.html
```
Expected: ~600+ lines.

- [ ] **Step 2: Copy it to the embedded location**

Run:
```bash
cp ui-preview/index.html internal/web/assets/index.html
```

- [ ] **Step 3: Edit the script section of `internal/web/assets/index.html`**

Find the mock data block (the `const mockStatus = { ... }` and `const mockScan = [ ... ]` declarations) and the calls that use them. Replace the three data-loading entry points with real `fetch` calls:

Replace:
```js
  renderStatus(mockStatus);
  renderScan(mockScan);
```
With:
```js
  // Fetch real status on load.
  fetch("/status").then(r => r.json()).then(renderStatus).catch(err => {
    showToast("error", "offline", "cannot reach daemon: " + err.message);
  });
  fetch("/scan").then(r => r.json()).then(data => renderScan(data.networks || [])).catch(() => {});
```

Replace the scan button handler body:
```js
    await new Promise(r => setTimeout(r, 900));
    renderScan(mockScan);
```
With:
```js
    try {
      const r = await fetch("/scan");
      const data = await r.json();
      renderScan(data.networks || []);
    } catch (err) {
      showToast("error", "scan failed", err.message);
    }
```

Replace the submit handler's mocked outcome block:
```js
    await new Promise(r => setTimeout(r, 2200));

    const ok = Math.random() > 0.25;
    btn.disabled = false;
    btn.classList.remove("loading");
    btn.textContent = original;

    if (ok) {
      showToast("success", "connected", `associated to ${ssid} · dhcp lease 192.168.1.42`);
      renderStatus({ ...mockStatus, ssid, link: "up" });
      $("#psk").value = "";
    } else {
      showToast("error", "failed", "authentication rejected by access point (4-way handshake timeout)");
      renderStatus({ ...mockStatus, link: "down", state: "disconnected", rssi: "—" });
    }
```
With:
```js
    let resp;
    try {
      resp = await fetch("/credentials", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ssid, psk })
      });
    } catch (err) {
      btn.disabled = false;
      btn.classList.remove("loading");
      btn.textContent = original;
      showToast("error", "network", "cannot reach daemon: " + err.message);
      return;
    }

    btn.disabled = false;
    btn.classList.remove("loading");
    btn.textContent = original;

    if (!resp.ok) {
      const text = await resp.text();
      showToast("error", "rejected", text);
      return;
    }
    const data = await resp.json();
    if (data.ok) {
      showToast("success", "connected", `associated to ${ssid} · ${data.status.ipv4 || "no ip"}`);
      renderStatus(data.status);
      $("#psk").value = "";
    } else {
      showToast("error", "failed", data.message);
      renderStatus(data.status);
    }
```

Also: since the backend returns `StatusResp` fields like `link`, `ssid`, `state`, `rssi`, `signal`, `ipv4`, `gateway`, `since`, `interface`, update `renderStatus` to match the server's shape. The preview already uses fields named `ssid`, `state`, `rssi`, `ip`, `gw`, `since`, `link`, `iface`, `signal` — rename the server-incoming fields to the preview's internal names, or change `renderStatus` to read server names. Pick the second (simpler). Change:

```js
  const renderStatus = (s) => {
    $("#cur-ssid").textContent  = s.ssid;
    const barsNode = $("#cur-bars");
    clear(barsNode);
    barsNode.appendChild(barsFor(s.signal));
    $("#cur-rssi").textContent  = s.rssi;
    $("#cur-state").textContent = s.state;
    $("#cur-ip").textContent    = s.ip;
    $("#cur-gw").textContent    = s.gw;
    $("#cur-since").textContent = s.since;
    $("#toplinkstate").textContent = s.link === "up" ? "associated" : "disconnected";
    $("#topiface").textContent  = s.iface;
    $("#topip").textContent     = s.ip.split(" ")[0];
    $("#topled").dataset.state  = s.link;
  };
```

To (read server field names):
```js
  const renderStatus = (s) => {
    $("#cur-ssid").textContent  = s.ssid || "—";
    const barsNode = $("#cur-bars");
    clear(barsNode);
    barsNode.appendChild(barsFor(s.signal || 0));
    $("#cur-rssi").textContent  = s.rssi || "—";
    $("#cur-state").textContent = s.state || "disconnected";
    $("#cur-ip").textContent    = s.ipv4 || "—";
    $("#cur-gw").textContent    = s.gateway || "—";
    $("#cur-since").textContent = s.since || "—";
    $("#toplinkstate").textContent = s.link === "up" ? "associated" : "disconnected";
    $("#topiface").textContent  = s.interface || "—";
    $("#topip").textContent     = (s.ipv4 || "").split("/")[0] || "—";
    $("#topled").dataset.state  = s.link === "up" ? "up" : "down";
  };
```

Also delete the `mockStatus` and `mockScan` consts entirely — they're no longer used.

- [ ] **Step 4: Verify the handler test still passes**

Run:
```bash
go test ./internal/web/...
```
Expected: PASS. The handler test for `/` only checks for the word `promised-lan` or `placeholder`, and the real file contains `promised-lan`, so it passes.

- [ ] **Step 5: Verify everything builds**

Run:
```bash
go build ./...
```
Expected: exit 0.

- [ ] **Step 6: Commit**

```bash
git add internal/web/assets/index.html
git commit -m "feat(web): ship real UI with live API wiring"
```

---

## Task 14: `cmd/promised-lan/main.go` + urfave/cli wiring

**Files:**
- Modify: `cmd/promised-lan/main.go`

- [ ] **Step 1: Add the dependency**

Run:
```bash
go get github.com/urfave/cli/v2
```

- [ ] **Step 2: Replace `cmd/promised-lan/main.go`**

Create (overwrite) `cmd/promised-lan/main.go`:
```go
package main

import (
	"log/slog"
	"os"

	"github.com/urfave/cli/v2"

	pcli "github.com/savaki/promised-lan/internal/cli"
)

const defaultConfigPath = "/etc/promised-lan/config.yaml"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	configFlag := &cli.StringFlag{
		Name:  "config",
		Usage: "path to config file",
		Value: defaultConfigPath,
	}

	app := &cli.App{
		Name:  "promised-lan",
		Usage: "manage the upstream WiFi link on a Raspberry Pi",
		Commands: []*cli.Command{
			pcli.ServeCommand(logger, configFlag),
			pcli.StatusCommand(logger, configFlag),
			pcli.ScanCommand(logger, configFlag),
			pcli.SetCommand(logger, configFlag),
			pcli.InstallCommand(logger),
			pcli.UninstallCommand(logger),
		},
	}

	if err := app.Run(os.Args); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}
```

This won't compile yet — the `pcli` package doesn't exist. That's fine; Tasks 15–21 create it.

- [ ] **Step 3: Commit the skeleton (it's OK that it doesn't build yet — we'll fix on next task)**

Skip this step — don't commit a broken build. Move directly to Task 15 and commit after the CLI package is populated.

---

## Task 15: `internal/cli/serve.go` — `serve` subcommand

**Files:**
- Create: `internal/cli/serve.go`

- [ ] **Step 1: Create `internal/cli/serve.go`**

Create `internal/cli/serve.go`:
```go
// Package cli implements the promised-lan subcommand handlers. Each exported
// function returns a *cli.Command that main.go wires into the urfave app.
package cli

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/savaki/promised-lan/internal/config"
	"github.com/savaki/promised-lan/internal/nm"
	"github.com/savaki/promised-lan/internal/web"
)

func ServeCommand(logger *slog.Logger, configFlag cli.Flag) *cli.Command {
	return &cli.Command{
		Name:  "serve",
		Usage: "run the promised-lan daemon",
		Flags: []cli.Flag{configFlag},
		Action: func(c *cli.Context) error {
			cfg, err := config.Load(c.String("config"))
			if err != nil {
				return err
			}
			client := nm.NewNMCLI(nm.NMCLIConfig{
				UpstreamInterface: cfg.UpstreamInterface,
				ConnectionName:    cfg.ConnectionName,
				ConnectTimeout:    time.Duration(cfg.ConnectTimeoutSeconds) * time.Second,
				Logger:            logger,
			})

			// Create profile at startup so it exists before any web request.
			startCtx, cancelStart := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancelStart()
			if err := client.EnsureProfile(startCtx); err != nil {
				logger.Error("ensure profile failed", "err", err)
				return err
			}

			srv := web.NewServer(cfg, client, logger)

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return srv.Run(ctx)
		},
	}
}
```

- [ ] **Step 2: Verify it compiles as a package alone**

Run:
```bash
go build ./internal/cli/...
```
Expected: exit 0 (the other subcommand files don't exist yet, so this only compiles serve.go).

Actually this will fail because main.go references `pcli.StatusCommand` etc. which don't exist yet. Skip the top-level build for now — defer verification to task 21.

- [ ] **Step 3: No commit yet — batch with remaining cli subcommands**

---

## Task 16: `internal/cli/status.go` and `internal/cli/scan.go`

**Files:**
- Create: `internal/cli/status.go`
- Create: `internal/cli/scan.go`

- [ ] **Step 1: Create `internal/cli/status.go`**

Create `internal/cli/status.go`:
```go
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/savaki/promised-lan/internal/config"
	"github.com/savaki/promised-lan/internal/nm"
)

func StatusCommand(logger *slog.Logger, configFlag cli.Flag) *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "print current upstream status",
		Flags: []cli.Flag{
			configFlag,
			&cli.BoolFlag{Name: "json", Usage: "emit JSON"},
		},
		Action: func(c *cli.Context) error {
			cfg, err := config.Load(c.String("config"))
			if err != nil {
				return err
			}
			client := nm.NewNMCLI(nm.NMCLIConfig{
				UpstreamInterface: cfg.UpstreamInterface,
				ConnectionName:    cfg.ConnectionName,
				ConnectTimeout:    time.Duration(cfg.ConnectTimeoutSeconds) * time.Second,
				Logger:            logger,
			})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			st, err := client.Status(ctx)
			if err != nil {
				return err
			}
			if c.Bool("json") {
				return json.NewEncoder(os.Stdout).Encode(st)
			}
			fmt.Printf("link:      %s\n", st.Link)
			fmt.Printf("interface: %s\n", st.Interface)
			fmt.Printf("ssid:      %s\n", st.SSID)
			fmt.Printf("state:     %s\n", st.State)
			fmt.Printf("ipv4:      %s\n", st.IPv4)
			fmt.Printf("gateway:   %s\n", st.Gateway)
			fmt.Printf("signal:    %d/5\n", st.Signal)
			if st.Link != "up" {
				os.Exit(1)
			}
			return nil
		},
	}
}
```

- [ ] **Step 2: Create `internal/cli/scan.go`**

Create `internal/cli/scan.go`:
```go
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/savaki/promised-lan/internal/config"
	"github.com/savaki/promised-lan/internal/nm"
)

func ScanCommand(logger *slog.Logger, configFlag cli.Flag) *cli.Command {
	return &cli.Command{
		Name:  "scan",
		Usage: "list nearby networks on the upstream radio",
		Flags: []cli.Flag{
			configFlag,
			&cli.BoolFlag{Name: "json", Usage: "emit JSON"},
		},
		Action: func(c *cli.Context) error {
			cfg, err := config.Load(c.String("config"))
			if err != nil {
				return err
			}
			client := nm.NewNMCLI(nm.NMCLIConfig{
				UpstreamInterface: cfg.UpstreamInterface,
				ConnectionName:    cfg.ConnectionName,
				ConnectTimeout:    time.Duration(cfg.ConnectTimeoutSeconds) * time.Second,
				Logger:            logger,
			})
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			nets, err := client.Scan(ctx)
			if err != nil {
				return err
			}
			if c.Bool("json") {
				return json.NewEncoder(os.Stdout).Encode(nets)
			}
			fmt.Printf("%-32s %-8s %-6s %s\n", "SSID", "SECURITY", "SIGNAL", "CURRENT")
			for _, n := range nets {
				cur := ""
				if n.Current {
					cur = "*"
				}
				fmt.Printf("%-32s %-8s %d/5    %s\n", n.SSID, n.Sec, n.Signal, cur)
			}
			return nil
		},
	}
}
```

- [ ] **Step 3: No commit yet**

---

## Task 17: `internal/cli/set.go`

**Files:**
- Create: `internal/cli/set.go`

- [ ] **Step 1: Create `internal/cli/set.go`**

Create `internal/cli/set.go`:
```go
package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/urfave/cli/v2"
	"golang.org/x/term"

	"github.com/savaki/promised-lan/internal/config"
	"github.com/savaki/promised-lan/internal/nm"
)

func SetCommand(logger *slog.Logger, configFlag cli.Flag) *cli.Command {
	return &cli.Command{
		Name:  "set",
		Usage: "update upstream credentials from the shell",
		Flags: []cli.Flag{
			configFlag,
			&cli.StringFlag{Name: "ssid", Usage: "SSID", Required: true},
			&cli.StringFlag{Name: "psk", Usage: "passphrase (use - to prompt)", Value: "-"},
		},
		Action: func(c *cli.Context) error {
			cfg, err := config.Load(c.String("config"))
			if err != nil {
				return err
			}
			psk := c.String("psk")
			if psk == "-" {
				if !term.IsTerminal(int(os.Stdin.Fd())) {
					return fmt.Errorf("--psk - requires a terminal")
				}
				fmt.Fprint(os.Stderr, "passphrase: ")
				b, err := term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Fprintln(os.Stderr)
				if err != nil {
					return err
				}
				psk = string(b)
			}
			if len(psk) < 8 || len(psk) > 63 {
				return fmt.Errorf("passphrase must be 8..63 bytes")
			}

			client := nm.NewNMCLI(nm.NMCLIConfig{
				UpstreamInterface: cfg.UpstreamInterface,
				ConnectionName:    cfg.ConnectionName,
				ConnectTimeout:    time.Duration(cfg.ConnectTimeoutSeconds) * time.Second,
				Logger:            logger,
			})

			ctx, cancel := context.WithTimeout(context.Background(),
				time.Duration(cfg.ConnectTimeoutSeconds+10)*time.Second)
			defer cancel()

			st, err := client.UpdateAndActivate(ctx, c.String("ssid"), psk)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed: %v\n", err)
				return err
			}
			fmt.Printf("connected: %s (%s)\n", st.SSID, st.IPv4)
			return nil
		},
	}
}
```

- [ ] **Step 2: Add `golang.org/x/term` dependency**

Run:
```bash
go get golang.org/x/term
```

- [ ] **Step 3: No commit yet**

---

## Task 18: `internal/cli/preflight.go` and tests

**Files:**
- Create: `internal/cli/preflight.go`
- Create: `internal/cli/preflight_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/cli/preflight_test.go`:
```go
package cli

import (
	"strings"
	"testing"

	"github.com/savaki/promised-lan/internal/config"
)

func TestPreflightAllPass(t *testing.T) {
	deps := PreflightDeps{
		IsRoot:         func() bool { return true },
		InterfaceExists: func(name string) bool { return true },
		NMActive:       func() bool { return true },
		NmcliOnPath:    func() bool { return true },
	}
	cfg := config.Config{UpstreamInterface: "wlan1", InteriorInterface: "wlan0"}
	if err := Preflight(cfg, deps); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestPreflightReportsAll(t *testing.T) {
	deps := PreflightDeps{
		IsRoot:         func() bool { return false },
		InterfaceExists: func(name string) bool { return false },
		NMActive:       func() bool { return false },
		NmcliOnPath:    func() bool { return false },
	}
	cfg := config.Config{UpstreamInterface: "wlan1", InteriorInterface: "wlan0"}
	err := Preflight(cfg, deps)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"root", "wlan1", "wlan0", "NetworkManager", "nmcli"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q: %s", want, msg)
		}
	}
}
```

- [ ] **Step 2: Run test — expect compile failure**

Run:
```bash
go test ./internal/cli/...
```
Expected: FAIL.

- [ ] **Step 3: Implement `internal/cli/preflight.go`**

Create `internal/cli/preflight.go`:
```go
package cli

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"

	"github.com/savaki/promised-lan/internal/config"
)

// PreflightDeps is a bag of dependency callbacks so preflight can be tested
// without touching the real system.
type PreflightDeps struct {
	IsRoot          func() bool
	InterfaceExists func(name string) bool
	NMActive        func() bool
	NmcliOnPath     func() bool
}

// DefaultPreflightDeps returns deps bound to the real system.
func DefaultPreflightDeps() PreflightDeps {
	return PreflightDeps{
		IsRoot: func() bool { return os.Geteuid() == 0 },
		InterfaceExists: func(name string) bool {
			_, err := net.InterfaceByName(name)
			return err == nil
		},
		NMActive: func() bool {
			return exec.Command("systemctl", "is-active", "--quiet", "NetworkManager").Run() == nil
		},
		NmcliOnPath: func() bool {
			_, err := exec.LookPath("nmcli")
			return err == nil
		},
	}
}

// Preflight runs all preflight checks, collecting every failure.
func Preflight(cfg config.Config, deps PreflightDeps) error {
	var errs []error
	if !deps.IsRoot() {
		errs = append(errs, errors.New("must run as root"))
	}
	if !deps.InterfaceExists(cfg.UpstreamInterface) {
		errs = append(errs, fmt.Errorf("upstream interface %s does not exist", cfg.UpstreamInterface))
	}
	if !deps.InterfaceExists(cfg.InteriorInterface) {
		errs = append(errs, fmt.Errorf("interior interface %s does not exist", cfg.InteriorInterface))
	}
	if !deps.NMActive() {
		errs = append(errs, errors.New("NetworkManager service is not active"))
	}
	if !deps.NmcliOnPath() {
		errs = append(errs, errors.New("nmcli not found on PATH"))
	}
	return errors.Join(errs...)
}
```

- [ ] **Step 4: Run tests — expect pass**

Run:
```bash
go test ./internal/cli/...
```
Expected: PASS (the other subcommand files need not exist for this test since it only imports `config`).

- [ ] **Step 5: No commit yet**

---

## Task 19: `internal/cli/install.go`

**Files:**
- Create: `internal/cli/promised-lan.service` (systemd unit, colocated with the code that embeds it so `go:embed` can find it — `go:embed` does not allow paths with `..`)
- Create: `internal/cli/install.go`

- [ ] **Step 1: Create the systemd unit file**

Create `internal/cli/promised-lan.service`:
```ini
[Unit]
Description=promised-lan upstream wifi controller
Documentation=https://github.com/savaki/promised-lan
After=NetworkManager.service network-online.target
Wants=NetworkManager.service

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/promised-lan serve --config /etc/promised-lan/config.yaml
Restart=on-failure
RestartSec=2s
StandardOutput=journal
StandardError=journal
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/etc/NetworkManager/system-connections
ProtectKernelTunables=true
ProtectKernelModules=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 2: Create `internal/cli/install.go`**

Create `internal/cli/install.go`:
```go
package cli

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/savaki/promised-lan/internal/config"
	"github.com/savaki/promised-lan/internal/nm"
)

//go:embed promised-lan.service
var systemdUnitContent []byte

const (
	installedBinaryPath = "/usr/local/bin/promised-lan"
	configDir           = "/etc/promised-lan"
	configPath          = "/etc/promised-lan/config.yaml"
	unitPath            = "/etc/systemd/system/promised-lan.service"
)

func InstallCommand(logger *slog.Logger) *cli.Command {
	return &cli.Command{
		Name:  "install",
		Usage: "install promised-lan as a systemd service",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "upstream-interface", Value: "wlan1"},
			&cli.StringFlag{Name: "interior-interface", Value: "wlan0"},
			&cli.IntFlag{Name: "http-port", Value: 8080},
			&cli.BoolFlag{Name: "force", Usage: "overwrite existing config"},
		},
		Action: func(c *cli.Context) error {
			cfg := config.Config{
				UpstreamInterface: c.String("upstream-interface"),
				InteriorInterface: c.String("interior-interface"),
				HTTPPort:          c.Int("http-port"),
			}
			// Apply defaults so Validate() sees complete config.
			tmp := cfg
			applyDefaultsForInstall(&tmp)
			if err := tmp.Validate(); err != nil {
				return err
			}

			deps := DefaultPreflightDeps()
			if err := Preflight(tmp, deps); err != nil {
				return err
			}

			if err := copyBinary(); err != nil {
				return fmt.Errorf("copy binary: %w", err)
			}
			if err := writeConfig(tmp, c.Bool("force")); err != nil {
				return fmt.Errorf("write config: %w", err)
			}
			if err := writeUnit(); err != nil {
				return fmt.Errorf("write unit: %w", err)
			}
			if err := systemctl("daemon-reload"); err != nil {
				return fmt.Errorf("daemon-reload: %w", err)
			}
			if err := systemctl("enable", "--now", "promised-lan"); err != nil {
				return fmt.Errorf("enable: %w", err)
			}

			// Create the NM profile so the first web request doesn't have to.
			client := nm.NewNMCLI(nm.NMCLIConfig{
				UpstreamInterface: tmp.UpstreamInterface,
				ConnectionName:    tmp.ConnectionName,
				ConnectTimeout:    time.Duration(tmp.ConnectTimeoutSeconds) * time.Second,
				Logger:            logger,
			})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := client.EnsureProfile(ctx); err != nil {
				logger.Warn("could not ensure NM profile yet (daemon will retry)", "err", err)
			}

			fmt.Printf("installed. Web UI will be at http://<interior-ip>:%d once the interior interface is up.\n", tmp.HTTPPort)
			return nil
		},
	}
}

// applyDefaultsForInstall is a copy of config.Config.applyDefaults intended
// for use by install, since applyDefaults is unexported in the config package.
// Keep in sync with internal/config/config.go.
func applyDefaultsForInstall(c *config.Config) {
	if c.UpstreamInterface == "" {
		c.UpstreamInterface = "wlan1"
	}
	if c.InteriorInterface == "" {
		c.InteriorInterface = "wlan0"
	}
	if c.HTTPPort == 0 {
		c.HTTPPort = 8080
	}
	if c.ConnectionName == "" {
		c.ConnectionName = "promised-lan-upstream"
	}
	if c.ConnectTimeoutSeconds == 0 {
		c.ConnectTimeoutSeconds = 20
	}
}

func copyBinary() error {
	src, err := os.Executable()
	if err != nil {
		return err
	}
	if filepath.Clean(src) == filepath.Clean(installedBinaryPath) {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(installedBinaryPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Chmod(0755)
}

func writeConfig(cfg config.Config, force bool) error {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}
	if _, err := os.Stat(configPath); err == nil && !force {
		return nil // leave existing config alone
	}
	data, err := cfg.Marshal()
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

func writeUnit() error {
	return os.WriteFile(unitPath, systemdUnitContent, 0644)
}

func systemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
```

- [ ] **Step 3: Verify it compiles**

Run:
```bash
go build ./internal/cli/...
```
Expected: exit 0 (only if Tasks 15–18 have been done; otherwise defer this check to Task 21).

- [ ] **Step 4: No commit yet**

---

## Task 20: `internal/cli/uninstall.go`

**Files:**
- Create: `internal/cli/uninstall.go`

- [ ] **Step 1: Create `internal/cli/uninstall.go`**

Create `internal/cli/uninstall.go`:
```go
package cli

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/urfave/cli/v2"
)

func UninstallCommand(logger *slog.Logger) *cli.Command {
	return &cli.Command{
		Name:  "uninstall",
		Usage: "remove promised-lan (binary, service, and optionally config/profile)",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "purge", Usage: "also remove config and NM profile"},
		},
		Action: func(c *cli.Context) error {
			if os.Geteuid() != 0 {
				return errors.New("must run as root")
			}
			var errs []error
			if err := systemctl("disable", "--now", "promised-lan"); err != nil {
				logger.Warn("systemctl disable failed (continuing)", "err", err)
			}
			if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
			if err := systemctl("daemon-reload"); err != nil {
				errs = append(errs, err)
			}
			if err := os.Remove(installedBinaryPath); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
			if c.Bool("purge") {
				if err := os.RemoveAll(configDir); err != nil {
					errs = append(errs, err)
				}
				// Best-effort: delete the NM profile. Don't fail if nmcli is missing.
				_ = exec.Command("nmcli", "con", "delete", "promised-lan-upstream").Run()
			}
			if len(errs) > 0 {
				return errors.Join(errs...)
			}
			fmt.Println("uninstalled.")
			return nil
		},
	}
}
```

- [ ] **Step 2: No commit yet**

---

## Task 21: Top-level build verification + full commit

- [ ] **Step 1: Build everything**

Run:
```bash
go build ./...
```
Expected: exit 0. If errors, fix imports or missing references.

- [ ] **Step 2: Run all non-integration tests**

Run:
```bash
go test ./...
```
Expected: PASS.

- [ ] **Step 3: Commit the whole CLI package + main.go + unit file**

```bash
git add cmd/promised-lan/main.go internal/cli/ go.sum go.mod
git commit -m "feat(cli): all subcommands (serve, status, scan, set, install, uninstall)"
```

---

## Task 22: Integration test against real nmcli (Linux only, build-tagged)

**Files:**
- Create: `internal/nm/nmcli_integration_test.go`

- [ ] **Step 1: Create `internal/nm/nmcli_integration_test.go`**

Create `internal/nm/nmcli_integration_test.go`:
```go
//go:build integration

package nm

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"testing"
	"time"
)

// Integration tests require:
//   - Linux host with NetworkManager running
//   - nmcli on PATH
//   - A wifi interface (real or virtual) — defaults to $NM_TEST_IFACE or "wlan0"
//
// Run: go test -tags=integration ./internal/nm/... -run Integration
//
// These tests use a disposable connection name `promised-lan-integration-test`
// and never touch the production `promised-lan-upstream` profile.

const testConn = "promised-lan-integration-test"

func requireNmcli(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("nmcli"); err != nil {
		t.Skip("nmcli not available")
	}
}

func testIface() string {
	if v := os.Getenv("NM_TEST_IFACE"); v != "" {
		return v
	}
	return "wlan0"
}

func TestIntegrationEnsureStatusScan(t *testing.T) {
	requireNmcli(t)
	iface := testIface()

	client := NewNMCLI(NMCLIConfig{
		UpstreamInterface: iface,
		ConnectionName:    testConn,
		ConnectTimeout:    10 * time.Second,
		Logger:            slog.Default(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.EnsureProfile(ctx); err != nil {
		t.Fatalf("EnsureProfile: %v", err)
	}
	// Cleanup on exit.
	t.Cleanup(func() {
		_ = exec.Command("nmcli", "con", "delete", testConn).Run()
	})

	if _, err := client.Status(ctx); err != nil {
		t.Fatalf("Status: %v", err)
	}
	nets, err := client.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	t.Logf("scan returned %d networks", len(nets))
}
```

- [ ] **Step 2: Verify it compiles with the integration tag**

Run:
```bash
go build -tags=integration ./internal/nm/...
```
Expected: exit 0.

- [ ] **Step 3: Verify it is excluded from the default build**

Run:
```bash
go test ./internal/nm/...
```
Expected: PASS (non-integration tests only).

- [ ] **Step 4: Commit**

```bash
git add internal/nm/nmcli_integration_test.go
git commit -m "test(nm): integration test skeleton guarded by build tag"
```

---

## Task 23: End-to-end smoke — build, run locally, hit /status

- [ ] **Step 1: Build the binary**

Run:
```bash
go build -o /tmp/promised-lan ./cmd/promised-lan
```
Expected: exit 0.

- [ ] **Step 2: Run it with a test config against the loopback interface**

Create a temporary config:
```bash
mkdir -p /tmp/promised-lan-test
cat > /tmp/promised-lan-test/config.yaml <<'YAML'
upstream_interface: lo
interior_interface: lo0
http_port: 18080
YAML
```
(Use `lo` on Linux, `lo0` on macOS — both may exist but only one will satisfy both roles. On a dev Mac this won't pass validation because upstream == interior is forbidden.)

For a real smoke test, use two loopback names by creating a dummy interface (Linux only):
```bash
# Linux only; skip on macOS
sudo ip link add dummy0 type dummy || true
sudo ip addr add 10.99.0.1/24 dev dummy0 || true
sudo ip link set dummy0 up || true
```
Then adjust the config:
```yaml
upstream_interface: dummy0
interior_interface: lo
http_port: 18080
```

On macOS dev machines this step is naturally skipped — run it on a Linux VM or the target Pi.

- [ ] **Step 3: Start the daemon in the background**

Run (Linux):
```bash
/tmp/promised-lan serve --config /tmp/promised-lan-test/config.yaml &
sleep 2
```

- [ ] **Step 4: Hit `/status`**

Run:
```bash
curl -s http://127.0.0.1:18080/status | jq .
```
Expected: JSON response with `link`, `ssid`, etc. (`link` will be `"down"` — that's fine; we're only testing that the HTTP plumbing works).

- [ ] **Step 5: Clean up**

Run:
```bash
kill %1
sudo ip link del dummy0 || true
```

- [ ] **Step 6: Commit README with smoke test instructions (optional)**

Skip unless the user requests a README.

---

## Self-review checklist

Before declaring done, verify against the spec:

- [ ] Every spec requirement (section 3) has at least one task
- [ ] The `nm.Client` interface from spec section 6.1 matches `internal/nm/client.go` exactly
- [ ] The JSON contracts from spec section 8.3 match `internal/web/json.go` exactly
- [ ] All six subcommands from spec section 11 exist
- [ ] The systemd unit from spec section 12 is embedded and written by install
- [ ] PSK is never logged (grep the codebase for `psk` after implementing — the only places it should appear are in function parameter names, the runner's redactIdx handling, and the configuration property names)
- [ ] Web server binds to interior IPv4, not `0.0.0.0` (verified in server.go line `net.Listen("tcp4", listenAddr)`)
- [ ] `go test ./...` passes
- [ ] `go build ./...` succeeds
