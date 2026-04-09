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
