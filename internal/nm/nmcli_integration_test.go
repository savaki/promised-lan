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
