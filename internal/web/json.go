// Package web serves the promised-lan web UI and JSON API, bound only to the
// interior network interface.
package web

import "github.com/savaki/promised-lan/internal/nm"

// StatusResp is the GET /status response body.
type StatusResp struct {
	Link      string `json:"link"`
	SSID      string `json:"ssid"`
	State     string `json:"state"`
	RSSI      string `json:"rssi"`
	Signal    int    `json:"signal"`
	IPv4      string `json:"ipv4"`
	Gateway   string `json:"gateway"`
	Since     string `json:"since"`
	Interface string `json:"interface"`
}

func newStatusResp(s nm.Status) StatusResp {
	since := ""
	if !s.Since.IsZero() {
		since = s.Since.UTC().Format("2006-01-02T15:04:05Z")
	}
	return StatusResp{
		Link:      s.Link,
		SSID:      s.SSID,
		State:     s.State,
		RSSI:      s.RSSI,
		Signal:    s.Signal,
		IPv4:      s.IPv4,
		Gateway:   s.Gateway,
		Since:     since,
		Interface: s.Interface,
	}
}

// ScanEntry is one network in a scan response.
type ScanEntry struct {
	SSID    string `json:"ssid"`
	Signal  int    `json:"signal"`
	Sec     string `json:"sec"`
	Current bool   `json:"current"`
}

// ScanResp is the GET /scan response body.
type ScanResp struct {
	Networks []ScanEntry `json:"networks"`
}

func newScanResp(nets []nm.Network) ScanResp {
	entries := make([]ScanEntry, 0, len(nets))
	for _, n := range nets {
		entries = append(entries, ScanEntry{
			SSID:    n.SSID,
			Signal:  n.Signal,
			Sec:     n.Sec,
			Current: n.Current,
		})
	}
	return ScanResp{Networks: entries}
}

// CredsReq is the POST /credentials request body.
type CredsReq struct {
	SSID string `json:"ssid"`
	PSK  string `json:"psk"`
}

// CredsResp is the POST /credentials response body.
type CredsResp struct {
	OK      bool       `json:"ok"`
	Status  StatusResp `json:"status"`
	Message string     `json:"message"`
}

// ErrResp is a plain error response for 4xx/5xx cases.
type ErrResp struct {
	Error string `json:"error"`
}
