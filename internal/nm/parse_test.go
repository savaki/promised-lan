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
