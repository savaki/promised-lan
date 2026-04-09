package nm

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
)

// parseTerse splits one line of `nmcli -t` output into fields. nmcli escapes
// literal ':' as `\:` and literal '\' as `\\`, and we unescape them here.
func parseTerse(line string) []string {
	var fields []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '\\' && i+1 < len(line) {
			next := line[i+1]
			if next == ':' || next == '\\' {
				cur.WriteByte(next)
				i++
				continue
			}
		}
		if c == ':' {
			fields = append(fields, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(c)
	}
	fields = append(fields, cur.String())
	return fields
}

// connectionProfile is the typed form of one `con show` row.
type connectionProfile struct {
	Name   string
	UUID   string
	Active string
	Device string
}

// parseConShow parses `nmcli -t -f NAME,UUID,ACTIVE,DEVICE con show [--active]`.
func parseConShow(data []byte) []connectionProfile {
	var out []connectionProfile
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		f := parseTerse(line)
		p := connectionProfile{}
		if len(f) > 0 {
			p.Name = f[0]
		}
		if len(f) > 1 {
			p.UUID = f[1]
		}
		if len(f) > 2 {
			p.Active = f[2]
		}
		if len(f) > 3 {
			p.Device = f[3]
		}
		out = append(out, p)
	}
	return out
}

// devShow is the typed form of `nmcli -t -f GENERAL.STATE,IP4.ADDRESS,IP4.GATEWAY,GENERAL.CONNECTION dev show <iface>`.
//
// IP4.ADDRESS in terse mode can be multi-valued as IP4.ADDRESS[1], IP4.ADDRESS[2], ...
// We take the first.
type devShow struct {
	State      string
	IPv4       string
	Gateway    string
	Connection string
}

func parseDevShow(data []byte) devShow {
	var ds devShow
	seenIPv4 := false
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		f := parseTerse(line)
		if len(f) < 2 {
			continue
		}
		key, val := f[0], f[1]
		switch {
		case key == "GENERAL.STATE":
			ds.State = val
		case key == "GENERAL.CONNECTION":
			ds.Connection = val
		case strings.HasPrefix(key, "IP4.ADDRESS") && !seenIPv4:
			ds.IPv4 = val
			seenIPv4 = true
		case key == "IP4.GATEWAY":
			ds.Gateway = val
		}
	}
	return ds
}

// parseDevWifiList parses `nmcli -t -f ACTIVE,SSID,SIGNAL,SECURITY dev wifi list`.
// Dedupes by SSID keeping the highest-signal row. Filters empty SSIDs.
func parseDevWifiList(data []byte) []Network {
	byName := make(map[string]Network)
	var order []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		f := parseTerse(line)
		if len(f) < 4 {
			continue
		}
		active, ssid, sigStr, sec := f[0], f[1], f[2], f[3]
		if ssid == "" {
			continue
		}
		sig, _ := strconv.Atoi(sigStr)
		n := Network{
			SSID:    ssid,
			Signal:  SignalBars(sig),
			Sec:     normalizeSec(sec),
			Current: active == "yes",
		}
		existing, ok := byName[ssid]
		if !ok {
			byName[ssid] = n
			order = append(order, ssid)
			continue
		}
		if n.Signal > existing.Signal {
			// Preserve Current flag: if either row claims current, entry is current.
			n.Current = n.Current || existing.Current
			byName[ssid] = n
		} else if n.Current {
			existing.Current = true
			byName[ssid] = existing
		}
	}
	out := make([]Network, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}
	return out
}

func normalizeSec(s string) string {
	s = strings.TrimSpace(s)
	switch {
	case s == "" || s == "--":
		return "open"
	case strings.Contains(s, "WPA3"):
		return "wpa3"
	case strings.Contains(s, "WPA2"), strings.Contains(s, "WPA1"), strings.Contains(s, "WPA"):
		return "wpa2"
	default:
		return strings.ToLower(s)
	}
}
