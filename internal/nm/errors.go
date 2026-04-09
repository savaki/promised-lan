package nm

import (
	"fmt"
	"strings"
)

// ErrorKind is a classification of nmcli failure modes based on exit code and stderr.
type ErrorKind int

const (
	ErrNone            ErrorKind = iota // success (exit 0)
	ErrUnknown                          // anything we don't recognize
	ErrTimeout                          // -w deadline expired (exit 3)
	ErrAuthRejected                     // 4-way handshake failure (exit 4, secrets required)
	ErrDHCPTimeout                      // associated but no IP lease (exit 4, ip-config-expired)
	ErrNoSecrets                        // no secret agent provided psk (exit 4, no secrets)
	ErrNMDown                           // NetworkManager not running (exit 8)
	ErrNetworkNotFound                  // connection/device/AP does not exist (exit 10)
	ErrInterfaceDown                    // upstream interface rfkilled or missing
)

// Error implements the error interface so ErrorKind values can be used directly
// or via errors.Is with NMError.
func (k ErrorKind) Error() string {
	switch k {
	case ErrNone:
		return "success"
	case ErrTimeout:
		return "timed out waiting for association"
	case ErrAuthRejected:
		return "authentication rejected by access point"
	case ErrDHCPTimeout:
		return "associated but no DHCP lease"
	case ErrNoSecrets:
		return "no passphrase available"
	case ErrNMDown:
		return "NetworkManager is not running"
	case ErrNetworkNotFound:
		return "network not found"
	case ErrInterfaceDown:
		return "upstream interface is down"
	default:
		return "unknown error"
	}
}

// NMError is the error type returned from Client operations.
type NMError struct {
	Kind   ErrorKind
	Stderr string
}

func (e *NMError) Error() string {
	if e.Stderr == "" {
		return e.Kind.Error()
	}
	return fmt.Sprintf("%s: %s", e.Kind.Error(), strings.TrimSpace(e.Stderr))
}

func (e *NMError) Unwrap() error { return e.Kind }

// Classify maps an nmcli exit code and stderr to an ErrorKind.
func Classify(exitCode int, stderr string) ErrorKind {
	if exitCode == 0 {
		return ErrNone
	}
	s := strings.ToLower(stderr)
	switch exitCode {
	case 3:
		return ErrTimeout
	case 4:
		switch {
		case strings.Contains(s, "ip-config-expired"):
			return ErrDHCPTimeout
		case strings.Contains(s, "no secrets"):
			return ErrNoSecrets
		case strings.Contains(s, "secrets were required"), strings.Contains(s, "4-way handshake"):
			return ErrAuthRejected
		default:
			return ErrUnknown
		}
	case 8:
		return ErrNMDown
	case 10:
		return ErrNetworkNotFound
	default:
		return ErrUnknown
	}
}
