# promised-lan

Self-installing Go binary that manages the **upstream WiFi credentials** on a Raspberry Pi with two radios. Exposes a password-less web UI reachable only from the interior network, so anyone on the interior side can change the upstream WiFi without SSH.

## What it does

A Raspberry Pi with two wifi radios — one built-in, one USB — acts as a tiny network gateway: the **built-in radio** is the **upstream client** (connects to whatever WiFi is around), and the **USB adapter** serves the **interior AP**. `promised-lan` is the thing that lets you change the upstream credentials from a phone on the interior wifi, without an SSH session.

- **Web UI** — amber-on-charcoal terminal aesthetic, bound to the interior interface only, no auth. Current status, nearby networks (click to prefill), credential form.
- **CLI** — `serve`, `status`, `scan`, `set`, `install`, `uninstall`. Manage from SSH if you prefer.
- **Self-installing** — `sudo ./promised-lan install` copies the binary, writes the config, writes a systemd unit, enables and starts the service. `uninstall` reverses every step.
- **NetworkManager-backed** — shells out to `nmcli` through a redacting command runner. Credentials live in NetworkManager's root-only storage; `promised-lan` never writes them to its own files or logs.

## Hardware & OS

- Raspberry Pi with two WiFi radios (built-in + USB adapter)
- Raspberry Pi OS Bookworm or newer (uses NetworkManager, not the older `dhcpcd` + `wpa_supplicant` stack)
- `nmcli` on `PATH` (default on Bookworm)

Default assumption:
- **`wlan0`** (built-in) → **upstream** (client mode)
- **`wlan1`** (USB) → **interior** (AP mode — configure separately with `hostapd` or NetworkManager shared mode)

**USB reorder warning:** USB adapters can renumber across reboots. Pin a stable name to the USB adapter's MAC via a `systemd.link` file or udev rule if this bites you.

`promised-lan` does **not** set up the interior AP itself. It assumes you've already configured `wlan1` as an AP serving DHCP to the interior network. It only manages the **upstream** side.

## Build & install

On any build host with Go 1.22+:

```bash
# Cross-compile for a 64-bit Pi (Pi 3/4/5)
GOOS=linux GOARCH=arm64 go build -o promised-lan ./cmd/promised-lan

# For 32-bit Pi (Pi Zero/1/2)
GOOS=linux GOARCH=arm GOARM=7 go build -o promised-lan ./cmd/promised-lan
```

Copy to the Pi and install:

```bash
scp promised-lan pi@raspberrypi.local:~/
ssh pi@raspberrypi.local

sudo ./promised-lan install
# Installed. Web UI will be at http://<interior-ip>:8080 once the interior interface is up.
```

The `install` subcommand:
1. Copies the binary to `/usr/local/bin/promised-lan`
2. Writes `/etc/promised-lan/config.yaml` (doesn't overwrite if one exists; pass `--force` to overwrite)
3. Writes `/etc/systemd/system/promised-lan.service`
4. Runs `systemctl daemon-reload` and `systemctl enable --now promised-lan`
5. Creates the NetworkManager connection profile `promised-lan-upstream`

Install flags (all optional):

```
--upstream-interface   interface for the upstream client (default "wlan0")
--interior-interface   interface the web UI binds to   (default "wlan1")
--http-port            web UI port                     (default 8080)
--force                overwrite existing config
```

## Usage

### Web UI

Connect to the interior WiFi on your phone or laptop, then open `http://<interior-ip>:8080`. The page shows:

- **Current upstream** status (SSID, signal, link state, IPv4, gateway, uptime)
- **Change credentials** form (SSID + passphrase, with show/hide)
- **Nearby networks** — scanned from the upstream radio; click a row to prefill the SSID field

Submitting credentials runs the full save-try-report flow: writes the new credentials to NetworkManager, attempts association, waits up to 20 seconds for a DHCP lease, and reports success or a specific failure reason (auth rejected, network not found, DHCP timeout, etc.).

### CLI

All subcommands read `/etc/promised-lan/config.yaml` unless you pass `--config`.

```bash
# Print current upstream status (exits 1 if down)
sudo promised-lan status
sudo promised-lan status --json

# List visible networks on the upstream radio
sudo promised-lan scan
sudo promised-lan scan --json

# Update credentials without touching the web UI
sudo promised-lan set --ssid MyWiFi
# passphrase: (prompted securely)

# Or inline (leaks to shell history — avoid)
sudo promised-lan set --ssid MyWiFi --psk mypassphrase

# Run the daemon in the foreground (systemd normally does this for you)
sudo promised-lan serve

# Remove everything install created
sudo promised-lan uninstall
sudo promised-lan uninstall --purge   # also remove config and NM profile
```

## Configuration

`/etc/promised-lan/config.yaml`:

```yaml
upstream_interface: wlan0         # built-in radio — the one we manage
interior_interface: wlan1         # USB adapter — where the web UI binds
http_port: 8080
connection_name: promised-lan-upstream
connect_timeout_seconds: 20
```

**No credentials** live in this file. SSID and PSK live in NetworkManager's root-only storage at `/etc/NetworkManager/system-connections/promised-lan-upstream.nmconnection`.

## How the security model works

This tool has no authentication. Access control is enforced by the **network topology**, not passwords:

1. **Interface-bound listener.** The web server binds to *exactly* the interior interface's IPv4 address, never `0.0.0.0`. The kernel drops any connection attempt from the upstream radio regardless of firewall state.
2. **Scoped writes.** Every NetworkManager operation targets the single profile `promised-lan-upstream`. The tool never enumerates or modifies other profiles. It's incapable of touching the interior AP's configuration.
3. **Secrets never land in our process.** New credentials flow through `promised-lan` for the duration of one HTTP request, into NetworkManager's storage, then out of memory. We never log, cache, or persist them.
4. **PSK redaction at the argv level.** The `nmcli` command runner redacts PSK positions to `***` before any log line is written, so `journalctl -u promised-lan` never contains a passphrase.
5. **XSS-resistant UI.** All dynamic DOM updates use `textContent` / `createElement` — no `innerHTML`. A hostile SSID broadcast nearby (e.g., `<script>alert(1)</script>`) cannot execute in the browser.

See the [design spec](docs/superpowers/specs/2026-04-09-promised-lan-wifi-manager-design.md) section 14 for the full threat model.

## Development

```bash
# Unit tests (fast, hermetic, no nmcli needed)
go test ./...

# Integration tests against a real nmcli (Linux only)
go test -tags=integration ./internal/nm/... -run Integration

# Build for local dev
go build ./cmd/promised-lan
```

Project layout:

```
cmd/promised-lan/main.go        urfave/cli/v2 wiring
internal/config/                YAML config loading + validation
internal/netiface/              interface name → IPv4 resolver
internal/nm/                    NetworkManager adapter (the only place nmcli lives)
internal/web/                   HTTP handlers, interface-bound server, embedded UI
internal/cli/                   all subcommand implementations + embedded systemd unit
docs/superpowers/
    specs/                      design doc
    plans/                      implementation plan
ui-preview/                     standalone HTML preview of the web UI
```

All NetworkManager interaction is confined to `internal/nm`. Everything else depends on the `nm.Client` interface and is tested against an in-memory `nm.Fake`.

## Troubleshooting

**Web UI unreachable.** Check `journalctl -u promised-lan` — the daemon polls the interior interface for up to 60 seconds at startup waiting for an IPv4 address. If the interior AP isn't up, the service fails to bind and systemd restarts it.

**Credentials rejected.** The error toast shows the specific failure kind:
- `authentication rejected by access point` — wrong passphrase
- `network not found` — SSID not visible from the upstream radio
- `associated but no DHCP lease` — connected to the AP but no IP; try a different network or check the AP's DHCP scope
- `timed out waiting for association` — the AP is flaky or out of range

**`sudo promised-lan install` fails.** The install subcommand runs preflight checks before touching anything. It will print *all* failures at once: not running as root, an interface doesn't exist, NetworkManager isn't active, `nmcli` not on PATH. Fix them and re-run.

**USB adapter keeps renaming.** Create `/etc/systemd/network/10-wlan-usb.link`:

```ini
[Match]
MACAddress=xx:xx:xx:xx:xx:xx

[Link]
Name=wlan-usb
```

Then set `interior_interface: wlan-usb` (or `upstream_interface`, depending on which radio is your USB) in the config.

## Non-goals

- WPA-Enterprise / EAP authentication
- Hidden SSIDs / open networks
- Multiple saved networks with priority fallback
- Automatic credential rollback on failure (the interior UI stays reachable, so it's unnecessary)
- Self-update / auto-upgrade
- IPv6 in the status display
- Managing the interior AP, hostapd, or DHCP server

See [design spec](docs/superpowers/specs/2026-04-09-promised-lan-wifi-manager-design.md) section 3 for the full list.
