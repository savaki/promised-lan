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

	// Apply new credentials. PSK value is at argv index 8:
	//   con(0) mod(1) <name>(2) 802-11-wireless.ssid(3) <ssid>(4)
	//   wifi-sec.key-mgmt(5) wpa-psk(6) wifi-sec.psk(7) <psk>(8)
	modArgs := []string{
		"con", "mod", n.connectionName,
		"802-11-wireless.ssid", ssid,
		"wifi-sec.key-mgmt", "wpa-psk",
		"wifi-sec.psk", psk,
	}
	if _, err := n.runner.run(ctx, modArgs, 8); err != nil {
		return Status{Link: "down", Interface: n.upstreamInterface}, err
	}

	// Bring it up with a bounded wait. The global -w flag must come before
	// the subcommand: `nmcli -w <N> con up <name>`.
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
