// Package netiface resolves a Linux network interface name to its current IPv4.
package netiface

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
)

// ErrNoInterface is returned when the named interface does not exist.
var ErrNoInterface = errors.New("interface not found")

// ErrNoIPv4 is returned when the interface exists but has no IPv4 address.
var ErrNoIPv4 = errors.New("interface has no IPv4 address")

// InterfaceIPv4 returns the first IPv4 address assigned to the named interface.
func InterfaceIPv4(name string) (netip.Addr, error) {
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("%w: %s: %v", ErrNoInterface, name, err)
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return netip.Addr{}, fmt.Errorf("read addrs for %s: %w", name, err)
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip4 := ipnet.IP.To4()
		if ip4 == nil {
			continue
		}
		addr, ok := netip.AddrFromSlice(ip4)
		if !ok {
			continue
		}
		return addr, nil
	}
	return netip.Addr{}, fmt.Errorf("%w: %s", ErrNoIPv4, name)
}
