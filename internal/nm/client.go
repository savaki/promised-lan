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
