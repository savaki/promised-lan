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
