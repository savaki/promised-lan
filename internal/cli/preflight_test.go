package cli

import (
	"strings"
	"testing"

	"github.com/savaki/promised-lan/internal/config"
)

func TestPreflightAllPass(t *testing.T) {
	deps := PreflightDeps{
		IsRoot:          func() bool { return true },
		InterfaceExists: func(name string) bool { return true },
		NMActive:        func() bool { return true },
		NmcliOnPath:     func() bool { return true },
	}
	cfg := config.Config{UpstreamInterface: "wlan1", InteriorInterface: "wlan0"}
	if err := Preflight(cfg, deps); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestPreflightReportsAll(t *testing.T) {
	deps := PreflightDeps{
		IsRoot:          func() bool { return false },
		InterfaceExists: func(name string) bool { return false },
		NMActive:        func() bool { return false },
		NmcliOnPath:     func() bool { return false },
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
