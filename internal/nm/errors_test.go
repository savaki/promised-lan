package nm

import (
	"errors"
	"testing"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name     string
		exitCode int
		stderr   string
		want     ErrorKind
	}{
		{"timeout exit 3", 3, "Timeout expired (30 seconds)", ErrTimeout},
		{"auth rejected", 4, "Error: Connection activation failed: (7) Secrets were required, but not provided.", ErrAuthRejected},
		{"no secrets", 4, "Error: Connection activation failed: no secrets", ErrNoSecrets},
		{"dhcp expired", 4, "Error: Connection activation failed: (32) ip-config-expired.", ErrDHCPTimeout},
		{"other activation failure", 4, "Error: Connection activation failed: unknown reason", ErrUnknown},
		{"nm down", 8, "Error: NetworkManager is not running.", ErrNMDown},
		{"does not exist", 10, "Error: unknown connection 'foo'.", ErrNetworkNotFound},
		{"unknown exit", 99, "mystery", ErrUnknown},
		{"success", 0, "", ErrNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.exitCode, tt.stderr)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestErrorKindError(t *testing.T) {
	err := &NMError{Kind: ErrAuthRejected, Stderr: "foo"}
	if !errors.Is(err, ErrAuthRejected) {
		t.Error("errors.Is must match ErrorKind sentinel")
	}
	if err.Error() == "" {
		t.Error("Error() must not be empty")
	}
}
