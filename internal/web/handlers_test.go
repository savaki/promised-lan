package web

import (
	"bytes"
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
