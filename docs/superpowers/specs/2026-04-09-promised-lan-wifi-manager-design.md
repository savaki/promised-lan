# promised-lan — Upstream WiFi Manager for Raspberry Pi

**Status:** draft — pending user approval
**Date:** 2026-04-09
**Target:** Raspberry Pi OS Bookworm (NetworkManager default)

---

## 1. Overview

`promised-lan` is a single Go binary that manages the **upstream WiFi link** on a Raspberry Pi with two radios (a built-in Broadcom radio used as the interior AP, and a USB adapter used as the upstream client). It exposes a password-less web UI reachable only from the interior network so that anyone on the interior side can change the upstream network credentials without SSH.

The binary is self-installing: `sudo ./promised-lan install` copies itself to `/usr/local/bin`, writes a default config, writes a systemd unit, and enables and starts the service. `uninstall` reverses every step.

**Guiding principle:** the binary's entire scope is to act as a thin, debuggable controller for *one* NetworkManager connection profile — `promised-lan-upstream` — pinned to the upstream radio. It never reads, modifies, or even observes any other profile. Secrets live in NetworkManager's storage, never in ours.

---

## 2. Hardware & OS Assumptions

- **Target OS:** Raspberry Pi OS Bookworm (or newer) with NetworkManager as the default network stack. Earlier releases that use `dhcpcd` + `wpa_supplicant` are explicitly out of scope.
- **Radios:**
  - Built-in Broadcom radio → interior (runs an AP via hostapd or NetworkManager shared mode, typically `wlan0`).
  - USB WiFi adapter → upstream client mode (typically `wlan1`).
- **Privilege model:** daemon runs as `root`. This matches the install story (single binary, no polkit rules to ship) and is sufficient because the whole point of the tool is to reconfigure networking.

**Deployment note (documented, not enforced by code):** USB adapters can reorder across reboots. Ship a README note recommending a `systemd.link` file or udev rule to pin a stable name to the upstream adapter's MAC. The tool does not create this rule itself.

---

## 3. Requirements

### Functional

1. Manage exactly one upstream WPA2/WPA3-PSK network via a single NetworkManager connection profile named `promised-lan-upstream`.
2. Provide a web UI (no authentication) bound only to the interior interface's IP.
3. Provide CLI subcommands: `serve`, `set`, `status`, `scan`, `install`, `uninstall`.
4. On credential update: write new credentials, attempt association, wait up to `connect_timeout_seconds` (default 20) for association + DHCP lease, report outcome back to the caller (web UI or CLI).
5. Auto-start on boot via an installed systemd unit.
6. Web UI must display: current SSID, signal strength, association state, IPv4 address/netmask, gateway, uptime.
7. Web UI must support a "scan" action listing nearby networks on the upstream radio with click-to-prefill.
8. Self-installing: `install` subcommand copies the binary, writes config, writes and enables the systemd unit.

### Non-functional

1. No authentication on the web UI. Access control is enforced by *binding only to the interior interface's IPv4 address* — not by firewall rules.
2. Credentials are never stored by the tool. They are written to NetworkManager's connection profile (`/etc/NetworkManager/system-connections/promised-lan-upstream.nmconnection`, mode 0600 root-only) and held in process memory only for the duration of a single request.
3. Credentials must never appear in logs. The `nm` package's command runner has an explicit redaction list for sensitive arguments.
4. The tool must be incapable of modifying the interior AP profile. This is enforced by scoping every operation to the `connection_name` defined in config; no code path enumerates or modifies other profiles.
5. All interaction with NetworkManager goes through a single `nm.Client` interface that wraps `nmcli`. No code outside `internal/nm` knows `nmcli` exists.

### Out of scope (explicit non-goals)

- WPA-Enterprise / EAP authentication.
- Hidden SSIDs, open networks (we ship WPA2/WPA3-PSK only in v1; the data model leaves room to add these later without migration).
- Multiple saved networks. One current profile, in place.
- Automatic credential rollback on failure. Web UI stays reachable from the interior, so the user can re-enter correct credentials and try again.
- Logging the PSK anywhere, ever.
- Managing the interior AP, hostapd, DHCP server, bridges, firewall rules, or anything other than the single upstream profile.
- Self-update / fetching new releases.

---

## 4. Architecture

### 4.1 Package layout

```
promised-lan/
├── cmd/promised-lan/
│   └── main.go                 // urfave/cli/v2 app wiring
├── internal/
│   ├── cli/
│   │   ├── serve.go            // runs daemon (config + web server)
│   │   ├── set.go              // SSH-side credential update
│   │   ├── status.go           // prints upstream status to stdout
│   │   ├── scan.go             // prints visible networks to stdout
│   │   ├── install.go          // install subcommand
│   │   ├── uninstall.go        // uninstall subcommand
│   │   └── preflight.go        // install preflight checks (unit-testable)
│   ├── config/
│   │   └── config.go           // YAML load / validate / defaults
│   ├── nm/
│   │   ├── client.go           // NMClient interface + real impl
│   │   ├── runner.go           // exec wrapper with PSK redaction
│   │   ├── parse.go            // -t -f output parsing
│   │   └── fake.go             // in-memory test double
│   ├── netiface/
│   │   └── bind.go             // resolve interface name → current IPv4
│   └── web/
│       ├── server.go           // http.Server, interior-bound listener
│       ├── handlers.go         // GET /, /status, /scan; POST /credentials
│       ├── json.go             // request/response structs
│       └── assets/
│           └── index.html      // embed via go:embed
└── systemd/
    └── promised-lan.service    // embedded unit template
```

### 4.2 Dependency rules

- `internal/nm` is the **only** package that imports `os/exec` for network operations or references `nmcli` in any string. Everything else depends on the `nm.Client` interface.
- `internal/config` imports nothing from this project. Pure YAML → struct with validation.
- `internal/netiface` imports only `net`. It exposes `func InterfaceIPv4(name string) (netip.Addr, error)`.
- `internal/web` takes its `nm.Client` via constructor (dependency injection) so tests can inject `nm.Fake`.
- `internal/cli` is the only package that wires the others together. Each subcommand file is: load config → build nm client → do the thing → exit.

### 4.3 Why these boundaries

- **Debuggability on embedded targets.** When something goes wrong on a Pi, every NetworkManager interaction is a shell command that can be replayed by hand. The logs of what we ran are equivalent to a reproducible test case.
- **Testability without NetworkManager.** `nm.Fake` implements the same interface in memory; CLI and web tests use it and never touch a real NetworkManager. Only the `internal/nm` package itself needs integration tests against real `nmcli`.
- **Security enforcement via scoping.** By making `connection_name` a config-level constant used by every `nm.Client` method, the tool cannot accidentally reference any other profile. There is no code path that lists all profiles or iterates over them.

---

## 5. Config file

`/etc/promised-lan/config.yaml`:

```yaml
# Network interfaces
upstream_interface: wlan1      # USB adapter — the one we manage
interior_interface: wlan0      # built-in — where the web UI binds

# Web UI
http_port: 8080

# NetworkManager connection profile name we own. Never touch other profiles.
connection_name: promised-lan-upstream

# How long to wait for new credentials to associate + get an IP before
# reporting failure back to the web UI.
connect_timeout_seconds: 20
```

### 5.1 Go type

```go
package config

type Config struct {
    UpstreamInterface     string `yaml:"upstream_interface"`
    InteriorInterface     string `yaml:"interior_interface"`
    HTTPPort              int    `yaml:"http_port"`
    ConnectionName        string `yaml:"connection_name"`
    ConnectTimeoutSeconds int    `yaml:"connect_timeout_seconds"`
}
```

### 5.2 Defaults (applied if field is zero value)

| Field                     | Default                     |
| ------------------------- | --------------------------- |
| `upstream_interface`      | `wlan1`                     |
| `interior_interface`      | `wlan0`                     |
| `http_port`               | `8080`                      |
| `connection_name`         | `promised-lan-upstream`     |
| `connect_timeout_seconds` | `20`                        |

### 5.3 Validation (at startup and at install time)

1. Both interfaces exist on the system (`ip link show <iface>`, or equivalent via `net.InterfaceByName`).
2. `upstream_interface != interior_interface` — hard failure.
3. `http_port` in `[1, 65535]`.
4. `connect_timeout_seconds` in `[5, 120]`.
5. `connection_name` non-empty, matches `^[a-zA-Z0-9._-]+$`.

Failure in any check prints all failures and exits with a non-zero code. No side effects.

### 5.4 What is *not* in the config

- No credentials. SSID and PSK live in NetworkManager's `/etc/NetworkManager/system-connections/promised-lan-upstream.nmconnection` only, which NetworkManager writes mode 0600 root-only and refuses to load if looser.
- No "last known good" credentials for rollback — we don't do rollback.

---

## 6. The `nm` package

The only package that knows about `nmcli`.

### 6.1 Interface

```go
package nm

type Client interface {
    // Status returns the current state of the upstream connection.
    // Returns (Status{Link: "down"}, nil) if the profile exists but is not active.
    // Returns an error only on nmcli/system failure, not on "not connected".
    Status(ctx context.Context) (Status, error)

    // Scan performs a rescan on the upstream interface and returns visible networks.
    Scan(ctx context.Context) ([]Network, error)

    // UpdateAndActivate writes new credentials to the managed profile and brings
    // it up. Blocks until either the connection is associated with an IP, the
    // context deadline fires, or association fails. Returns the final Status
    // and any error.
    //
    // The psk is treated as a secret: it is passed to nmcli via a fresh argv
    // slice whose logged form has the psk replaced with "***".
    UpdateAndActivate(ctx context.Context, ssid, psk string) (Status, error)

    // EnsureProfile creates the managed profile if it doesn't exist yet.
    // Idempotent. Called at daemon startup and by `install`.
    EnsureProfile(ctx context.Context) error
}

type Status struct {
    Link      string    // "up" | "down"
    SSID      string    // current associated SSID, if any
    State     string    // e.g. "associated · wpa2-psk" (human-readable, from nmcli)
    RSSI      string    // e.g. "-52 dBm" — string because nmcli formats vary
    Signal    int       // 0..5 bars, computed from nmcli SIGNAL percentage
    IPv4      string    // "192.168.1.42/24" or "" if none
    Gateway   string    // "192.168.1.1" or ""
    Since     time.Time // connection start time, zero if down
    Interface string    // upstream interface name
}

type Network struct {
    SSID    string
    Signal  int    // 0..5 bars
    Sec     string // "open", "wpa2", "wpa3"
    Current bool   // true if this SSID matches the active profile
}
```

### 6.2 nmcli command shapes

All commands use `-t` (terse) and `-f` (field list) for stable, parseable output. Never parse human-readable `nmcli` output. In the examples below, `wlan1` and `promised-lan-upstream` are illustrative; real invocations substitute `cfg.UpstreamInterface` and `cfg.ConnectionName`.

**Ensure profile exists (idempotent):** create the profile with security settings in one shot so there is never a transient insecure profile on disk. The `placeholder` values get overwritten on the first real credential update.
```
nmcli -t -f NAME con show | grep -Fx promised-lan-upstream
# if absent:
nmcli con add type wifi ifname wlan1 con-name promised-lan-upstream \
  ssid placeholder \
  wifi-sec.key-mgmt wpa-psk \
  wifi-sec.psk placeholder-psk \
  connection.autoconnect yes \
  connection.interface-name wlan1
```

**Update credentials (PSK redacted in logs):**
```
nmcli con mod promised-lan-upstream \
  802-11-wireless.ssid "<SSID>" \
  wifi-sec.key-mgmt wpa-psk \
  wifi-sec.psk "<PSK>"
```
Multiple `setting.property value` pairs in one `con mod` invocation are applied atomically. `wifi-sec` is an alias for `802-11-wireless-security`.

**Activate profile:**
```
nmcli -w <connect_timeout_seconds> con up promised-lan-upstream
```
The global `-w` / `--wait` flag (before the subcommand) sets the maximum seconds nmcli will block waiting for the activation to complete. Exit codes we care about:

| Exit code | Meaning                                                             |
| --------- | ------------------------------------------------------------------- |
| 0         | Success — connection reached `activated` state                     |
| 3         | Timeout expired before activation completed                        |
| 4         | Activation failed (authentication, no route, etc.)                 |
| 8         | NetworkManager is not running                                       |
| 10        | Connection / device / access point does not exist                   |

The `activated` state in practice means IP configuration was applied (L2 association + L3 success), but the nmcli manpage does not explicitly guarantee this — we defensively verify by calling `Status()` after `con up` returns and treat `Link=="up"` only if both SSID association AND a non-empty `IP4.ADDRESS` are present. Never trust the exit code alone.

**Deactivate (used only during testing, not in normal flow):**
```
nmcli con down promised-lan-upstream
```

**Status:**
```
nmcli -t -f GENERAL.STATE,IP4.ADDRESS,IP4.GATEWAY,GENERAL.CONNECTION dev show wlan1
nmcli -t -f NAME,UUID,ACTIVE,DEVICE con show --active
nmcli -t -f ACTIVE,SSID,SIGNAL,SECURITY dev wifi list ifname wlan1
```
Status is composed from the union of these three queries, filtered by matching `connection_name` / `upstream_interface`. **Parse note:** in terse mode, `IP4.ADDRESS` can appear multiple times with indexed keys (`IP4.ADDRESS[1]`, `IP4.ADDRESS[2]`, ...) when the interface has multiple addresses. The parser takes the first (`[1]`) and ignores the rest; same for `IP4.DNS[n]`. Same treatment for `IP4.ROUTE[n]` if we ever need it.

**Scan:**
```
nmcli -t -f ACTIVE,SSID,SIGNAL,SECURITY dev wifi list ifname wlan1 --rescan yes
```
We use the single `dev wifi list --rescan yes` form rather than the two-step `rescan` + `list` because:

1. `dev wifi rescan` is **rate-limited by NetworkManager** to roughly once every 30 seconds. Calling it too soon returns a non-zero exit with "Scanning not allowed immediately following previous scan". We'd have to either swallow that error or sleep — both awkward.
2. `dev wifi list --rescan yes` does the right thing: it triggers a rescan if allowed, waits for results, and returns the merged list. If the rate limit prevents a rescan, it returns the cached list without erroring. That's the behavior we want for the web UI's "scan" button.

Post-processing:

- Rows with empty SSID (hidden networks) are filtered out in v1.
- `dev wifi list` returns one row per BSS (one per AP/band combination), so the same SSID can appear multiple times. We dedupe by SSID, keeping the row with the highest SIGNAL.
- The `ACTIVE` column returns `yes`/`no` and is the source of truth for "current" — it's what the upstream radio is actually associated to, not what the profile *claims*.

### 6.3 Command runner with redaction

```go
// runner.go
type runner struct {
    logger *slog.Logger
}

// run executes nmcli with args. redactIdx lists argv positions whose values
// should be replaced with "***" in any logged form. Returns stdout.
func (r *runner) run(ctx context.Context, args []string, redactIdx ...int) ([]byte, error) {
    logArgs := make([]string, len(args))
    copy(logArgs, args)
    for _, i := range redactIdx {
        if i >= 0 && i < len(logArgs) {
            logArgs[i] = "***"
        }
    }
    r.logger.Debug("nmcli", "args", logArgs)

    cmd := exec.CommandContext(ctx, "nmcli", args...)
    var stderr bytes.Buffer
    cmd.Stderr = &stderr
    out, err := cmd.Output()
    if err != nil {
        return out, fmt.Errorf("nmcli failed: %w: %s", err, stderr.String())
    }
    return out, nil
}
```

The `UpdateAndActivate` implementation calls `run(ctx, args, pskIdx)` with the index of the PSK value in `args`, guaranteeing the PSK never reaches any logger.

### 6.4 Parsing

`nmcli -t` emits one record per line with `:` as field separator; values containing `:` are backslash-escaped. The `parse.go` file exposes one helper:

```go
func parseTerse(line string) []string
```

that handles the escape rules correctly, plus small typed parsers for each command's output.

### 6.5 Signal mapping

`nmcli` returns signal as a 0-100 integer. We map to 0-5 bars for display:

| nmcli signal | bars |
| ------------ | ---- |
| 0            | 0    |
| 1-20         | 1    |
| 21-40        | 2    |
| 41-60        | 3    |
| 61-80        | 4    |
| 81-100       | 5    |

### 6.6 Fake

`nm.Fake` is an in-memory implementation of `nm.Client` used by every package test above `internal/nm`. It holds a small struct representing the "current" state and returns canned data, with a `SetStatus`/`SetScan` API for tests to stage scenarios.

---

## 7. The `netiface` package

```go
package netiface

// InterfaceIPv4 returns the first IPv4 address currently assigned to the
// named interface. Returns a wrapped os.ErrNotExist if the interface has no
// IPv4 yet, so callers can poll.
func InterfaceIPv4(name string) (netip.Addr, error)
```

Implementation uses `net.InterfaceByName(name).Addrs()` and filters for IPv4. No shelling out, no dependencies beyond stdlib.

Used by:
- `internal/web/server.go` to determine the bind address.
- `internal/cli/status.go` to print the interior address.
- `internal/cli/install.go` to print the web UI URL after installation.

---

## 8. The `web` package

### 8.1 Server lifecycle and binding

```go
package web

type Server struct {
    cfg    config.Config
    nm     nm.Client
    logger *slog.Logger
}

func (s *Server) Run(ctx context.Context) error
```

`Run` does:

1. Poll `netiface.InterfaceIPv4(s.cfg.InteriorInterface)` every 500ms (up to 60s) until it returns an IPv4. If the timeout expires, return an error that systemd will log — the unit will restart us and try again.
2. Build a `net.Listener` bound to exactly `<interior-ipv4>:<http_port>`. Never `0.0.0.0`.
3. Start an `http.Server` with the handlers below, serving until `ctx` is canceled.
4. On context cancellation, call `srv.Shutdown(ctx)` with a short grace period.

**Why bind rather than firewall:** binding to a specific IP means the kernel will not accept connections on that port from any other interface, regardless of firewall state. A firewall rule can be flushed, a bind cannot.

**What if the interior interface changes IP?** The daemon logs a warning and exits; systemd restarts it, and it re-binds to the new address. The alternative (watching netlink for address changes and rebinding in-process) is complexity we don't need — interior AP IPs are stable in practice, and a restart takes under a second.

### 8.2 Handlers

| Method | Path            | Purpose                                        |
| ------ | --------------- | ---------------------------------------------- |
| GET    | `/`             | Serve embedded `index.html`                    |
| GET    | `/status`       | Return current `Status` as JSON                |
| GET    | `/scan`         | Trigger scan, return `[]Network` as JSON       |
| POST   | `/credentials`  | Update creds, attempt association, return result |

### 8.3 JSON contracts

```go
// GET /status response
type StatusResp struct {
    Link      string `json:"link"`       // "up" | "down"
    SSID      string `json:"ssid"`
    State     string `json:"state"`
    RSSI      string `json:"rssi"`
    Signal    int    `json:"signal"`     // 0..5
    IPv4      string `json:"ipv4"`       // "192.168.1.42/24"
    Gateway   string `json:"gateway"`
    Since     string `json:"since"`      // RFC3339 or ""
    Interface string `json:"interface"`
}

// GET /scan response
type ScanResp struct {
    Networks []ScanEntry `json:"networks"`
}
type ScanEntry struct {
    SSID    string `json:"ssid"`
    Signal  int    `json:"signal"`   // 0..5
    Sec     string `json:"sec"`      // "open" | "wpa2" | "wpa3"
    Current bool   `json:"current"`
}

// POST /credentials request
type CredsReq struct {
    SSID string `json:"ssid"`
    PSK  string `json:"psk"`
}

// POST /credentials response
type CredsResp struct {
    OK      bool        `json:"ok"`
    Status  StatusResp  `json:"status"`
    Message string      `json:"message"` // human-readable success or error detail
}
```

### 8.4 Handler validation

- `POST /credentials`: SSID 1–32 bytes, PSK 8–63 bytes (WPA2 spec). Reject with 400 and a plain JSON error if out of range.
- All handlers enforce `Content-Type: application/json` for POST. Otherwise 415.
- CORS is not enabled. Same-origin by design.
- No CSRF protection (the UI is unauthenticated and there's nothing to cross-site; a successful attack would require being on the interior network, at which point the attacker already has full control).

### 8.5 Embedded assets

`//go:embed assets/index.html` embeds the single HTML file. No external CDN fonts in the final served version (the design preview uses Google Fonts, but the shipped binary will bundle IBM Plex Mono as a subset `.woff2` embedded via `go:embed` and served at `/assets/plex-mono.woff2` — this avoids a hard dependency on outbound internet from the Pi, which may not be connected yet when a user is fixing the upstream credentials).

---

## 9. Save, try, report — the core flow

When the user submits new credentials:

```
┌────────────────────────────────────────────────────────────────────────┐
│ 1. POST /credentials arrives with {ssid, psk}                          │
│                                                                        │
│ 2. Validate shape. Reject immediately if invalid.                      │
│                                                                        │
│ 3. Build a context with deadline = now + connect_timeout_seconds.      │
│                                                                        │
│ 4. Call nm.Client.UpdateAndActivate(ctx, ssid, psk). Internally:       │
│    a. nmcli con mod <name> ... ssid ... psk ...                        │
│    b. nmcli con up <name> --timeout <connect_timeout_seconds>          │
│       which blocks until associated-with-IP OR fails OR times out.     │
│    c. After `con up` returns, call Status() to get the real state.    │
│                                                                        │
│ 5. Response:                                                           │
│    - OK=true  if Status.Link == "up" and IPv4 != ""                    │
│    - OK=false otherwise, with a message derived from the nmcli error   │
│      (e.g. "authentication rejected", "no secrets available",          │
│       "network not found", "timed out waiting for DHCP").              │
└────────────────────────────────────────────────────────────────────────┘
```

### 9.1 Error classification

The `nm` package translates nmcli exit codes and stderr into a small set of user-meaningful messages. Exit code is the primary signal (stable across versions); stderr substring matching is only used to discriminate within a single exit code. Classification lives in `internal/nm/errors.go`:

```go
type ErrorKind int
const (
    ErrUnknown ErrorKind = iota
    ErrAuthRejected       // 4-way handshake failure
    ErrNetworkNotFound    // SSID not visible / profile missing
    ErrDHCPTimeout        // associated but no lease
    ErrNoSecrets          // agent didn't provide psk (shouldn't happen)
    ErrInterfaceDown      // upstream interface is rfkilled or missing
    ErrNMDown             // NetworkManager service not running
    ErrTimeout            // -w deadline expired before activation completed
)
```

**Classification table:**

| nmcli exit code | Stderr substring (if used)             | ErrorKind          |
| --------------- | -------------------------------------- | ------------------ |
| 3               | (any)                                  | `ErrTimeout`       |
| 4               | contains `secrets were required`       | `ErrAuthRejected`  |
| 4               | contains `(32) ip-config-expired`      | `ErrDHCPTimeout`   |
| 4               | contains `no secrets`                  | `ErrNoSecrets`     |
| 4               | (other)                                | `ErrUnknown`       |
| 8               | (any)                                  | `ErrNMDown`        |
| 10              | (any)                                  | `ErrNetworkNotFound` |
| other           | (any)                                  | `ErrUnknown`       |

Additionally, after a successful `con up` (exit 0), we still call `Status()` and classify as `ErrUnknown` with the raw state string if `Link != "up"` — defensive because the manpage doesn't explicitly guarantee that exit 0 means full L3 readiness.

The substring list is small, bounded, and captured in `internal/nm/errors_test.go` as table-driven tests against real nmcli error samples collected from the field.

### 9.2 What we explicitly don't do

- **No rollback.** If the new credentials fail, the profile keeps the new credentials. The interior UI stays reachable (because it's on a different radio), and the user enters correct ones.
- **No retries.** One attempt per request. If the user wants to retry they click the button again.
- **No background reconnect logic.** NetworkManager's autoconnect handles that after we've successfully set credentials once.

---

## 10. Web UI

A single embedded `index.html` implementing the design in `ui-preview/index.html` (committed as part of this spec work). Key design decisions:

- **Aesthetic:** network-appliance admin panel — warm amber (`#ffb347`) on deep charcoal (`#0b0d0c`), IBM Plex Mono throughout, L-shaped corner brackets on panels, LED status dot with layered glow, ASCII `▁▂▃▄▅` signal bars, faint scanlines + top amber glow for atmosphere.
- **Layout:** single column, max-width 880px, mobile-first with a 640px breakpoint scaling down.
- **Panels, top to bottom:** status bar (link/iface/ip), current upstream (kv list), change credentials (form), nearby networks (scannable list).
- **Interactions:**
  - Clicking a scan result prefills the SSID field and focuses the passphrase input.
  - Scan button re-runs on demand with a "scanning…" state.
  - Submit runs the full save-try-report flow with a pending toast, then success/error toast.
  - Show/hide toggle on the passphrase input.
- **Security:**
  - All dynamic content rendered via `createElement` + `textContent`. No `innerHTML` anywhere — SSIDs are attacker-controlled (anyone nearby can broadcast a hostile SSID) and must never be interpreted as HTML.
  - Same-origin only, no CORS.
- **Accessibility:** `prefers-reduced-motion` disables all animations including the caret blink. `aria-live="polite"` on the status bar. All inputs have labels. Touch targets ≥ 44px.

The preview file is a working mockup with randomized success/failure outcomes so it can be reviewed in a browser without running the backend.

---

## 11. CLI subcommands

Built with `urfave/cli/v2`.

### 11.1 `promised-lan serve`

Runs the daemon. Loads config, builds `nm.Client`, starts the web server. Logs to stdout (systemd captures to journal).

Flags:
- `--config <path>` (default `/etc/promised-lan/config.yaml`)

### 11.2 `promised-lan set --ssid FOO --psk BAR`

Update credentials from the shell. Same `UpdateAndActivate` flow as the web UI, prints the result to stdout. Useful for first-time setup before you know the interior IP.

Flags:
- `--ssid` (required)
- `--psk` (required) — prompts via `term.ReadPassword` if stdin is a terminal and the flag is omitted with `--psk -`
- `--config <path>`

### 11.3 `promised-lan status`

Prints current status as a human-readable block, plus the interior UI URL. Exit code 0 if link is up, 1 otherwise.

Flags:
- `--config <path>`
- `--json` — emit the same shape as `GET /status`

### 11.4 `promised-lan scan`

Triggers a scan on the upstream radio, prints visible networks as a table. No web server needed.

Flags:
- `--config <path>`
- `--json`

### 11.5 `promised-lan install`

Self-installing bootstrap. Order of operations:

1. **Preflight** (bails out with all failures printed if any fail):
   - Running as root
   - Both listed interfaces exist
   - NetworkManager service is active
   - `nmcli` is on PATH
2. **Copy binary** — `os.Executable()` → `/usr/local/bin/promised-lan`, mode 0755. Skip if source and dest are the same file (idempotent re-install).
3. **Write config** — at `/etc/promised-lan/config.yaml` with values from flags. Skip if the file already exists unless `--force`.
4. **Write systemd unit** — at `/etc/systemd/system/promised-lan.service`, contents from embedded template.
5. **`systemctl daemon-reload`**
6. **`systemctl enable --now promised-lan`**
7. **`EnsureProfile`** — create the NM connection profile if it doesn't exist (idempotent).
8. **Print success** with the interior UI URL.

Flags:
- `--upstream-interface` (default `wlan1`)
- `--interior-interface` (default `wlan0`)
- `--http-port` (default `8080`)
- `--force` — overwrite existing config

### 11.6 `promised-lan uninstall`

Reverse in reverse order: `systemctl disable --now`, remove unit file, `daemon-reload`, remove the binary from `/usr/local/bin/promised-lan`. **Leaves `/etc/promised-lan/config.yaml` alone** unless `--purge` is passed. Does not remove the NetworkManager connection profile unless `--purge`.

Flags:
- `--purge` — also remove config and NM profile

---

## 12. Systemd unit

Embedded in the binary via `//go:embed systemd/promised-lan.service`:

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

# Hardening: everything we don't need
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

The hardening block is a best-effort sandbox. `ReadWritePaths=/etc/NetworkManager/system-connections` is required because `nmcli con mod` causes NetworkManager (not our process) to rewrite the `.nmconnection` file, but our binary also reads the interior IP from netlink which works fine under `ProtectSystem=strict`.

---

## 13. Testing strategy

### 13.1 Unit tests (no filesystem, no nmcli)

- `internal/config`: table-driven validation tests.
- `internal/nm/parse.go`: golden-file tests against captured `nmcli -t -f` output (real samples committed in `testdata/`).
- `internal/nm/errors.go`: table test mapping nmcli error strings to `ErrorKind`.
- `internal/web`: handler tests using `httptest.NewServer` and `nm.Fake` — covers success, validation failure, and each `ErrorKind` from the fake.
- `internal/cli/preflight.go`: inject fake dependency functions, test each failure case.
- `internal/netiface`: test against `net.InterfaceByName` on `lo` which always exists.

### 13.2 Integration tests (require a Linux box with NetworkManager)

Guarded behind `//go:build integration`. Runs against real `nmcli`:

- Create a dummy profile named `promised-lan-integration-test`, modify it, activate/deactivate, delete it. Never touches the `promised-lan-upstream` profile name used in production.
- Skipped on macOS CI; run manually on a Pi or in a Docker container with NetworkManager installed.

### 13.3 Manual test plan (Pi hardware)

Documented in the README, not automated:

1. `sudo ./promised-lan install` on a fresh Pi — verify service active, web UI reachable.
2. Open web UI, change credentials to a correct wifi — verify success toast, status updates.
3. Change to wrong credentials — verify error toast, status shows down, interior still works.
4. Reboot — verify service comes back up and upstream reconnects.
5. `sudo ./promised-lan uninstall` — verify everything is removed.

---

## 14. Security review

| Concern                                  | Mitigation                                                                                              |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| PSK leaked via logs                      | Redaction layer in `nm/runner.go`, enforced by interface requiring `redactIdx`                          |
| PSK leaked via `/status`                 | `Status` struct has no PSK field. The `.nmconnection` file is root-only                                 |
| Web UI exposed to upstream network       | Bind to interior interface's exact IP, not `0.0.0.0`. Verified every restart                            |
| XSS via hostile SSID in scan results     | All UI rendering uses `textContent` / `createElement`. No `innerHTML`                                   |
| Reconfigure of interior AP profile       | All operations scoped to `connection_name`. No enumeration                                               |
| Pre-install binary tampering             | Out of scope — the install script assumes the operator trusts the binary they copied to the Pi         |
| Denial of interior network via bad creds | Acceptable — interior UI stays reachable, user retries                                                  |
| Daemon running as root                   | Trade-off accepted. Sandboxed via systemd directives. Scope is narrow by design                         |

---

## 15. Open questions / decisions deferred

None blocking v1. Future considerations noted for posterity:

- Multiple saved networks: add a `connection_name` list in config, expose profile selection in UI.
- Hidden SSIDs / open networks: add a small form variant, set `802-11-wireless.hidden yes` or `wifi-sec.key-mgmt none`.
- WPA-Enterprise: significant UI and data-model work, likely a separate project.
- Credential rollback: only worth adding if we discover real users getting locked out, which shouldn't happen given the interior-UI architecture.
- Auto-update: out of scope for v1.
- IPv6: status display shows IPv4 only for now.

---

## 16. Acceptance criteria

The implementation is done when:

1. `sudo ./promised-lan install` on a fresh Pi OS Bookworm + NetworkManager + two radios produces a running service reachable from the interior wifi.
2. Opening the web UI from a phone on the interior wifi shows current status, nearby networks, and the credential form.
3. Entering correct credentials associates the upstream within ~20s and shows a success toast with the new IP.
4. Entering wrong credentials shows a specific error toast (auth rejected, network not found, etc.) and the interior UI remains reachable.
5. Rebooting the Pi brings the service back and reconnects the upstream.
6. `sudo ./promised-lan uninstall` removes the service, unit, and binary.
7. No PSK appears in `journalctl -u promised-lan` for any scenario.
8. Unit tests pass with `go test ./...`. Integration tests pass with `go test -tags=integration ./internal/nm/...` on a Linux host with NetworkManager.
