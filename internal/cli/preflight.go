package cli

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"

	"github.com/savaki/promised-lan/internal/config"
)

// PreflightDeps is a bag of dependency callbacks so preflight can be tested
// without touching the real system.
type PreflightDeps struct {
	IsRoot          func() bool
	InterfaceExists func(name string) bool
	NMActive        func() bool
	NmcliOnPath     func() bool
}

// DefaultPreflightDeps returns deps bound to the real system.
func DefaultPreflightDeps() PreflightDeps {
	return PreflightDeps{
		IsRoot: func() bool { return os.Geteuid() == 0 },
		InterfaceExists: func(name string) bool {
			_, err := net.InterfaceByName(name)
			return err == nil
		},
		NMActive: func() bool {
			return exec.Command("systemctl", "is-active", "--quiet", "NetworkManager").Run() == nil
		},
		NmcliOnPath: func() bool {
			_, err := exec.LookPath("nmcli")
			return err == nil
		},
	}
}

// Preflight runs all preflight checks, collecting every failure.
func Preflight(cfg config.Config, deps PreflightDeps) error {
	var errs []error
	if !deps.IsRoot() {
		errs = append(errs, errors.New("must run as root"))
	}
	if !deps.InterfaceExists(cfg.UpstreamInterface) {
		errs = append(errs, fmt.Errorf("upstream interface %s does not exist", cfg.UpstreamInterface))
	}
	if !deps.InterfaceExists(cfg.InteriorInterface) {
		errs = append(errs, fmt.Errorf("interior interface %s does not exist", cfg.InteriorInterface))
	}
	if !deps.NMActive() {
		errs = append(errs, errors.New("NetworkManager service is not active"))
	}
	if !deps.NmcliOnPath() {
		errs = append(errs, errors.New("nmcli not found on PATH"))
	}
	return errors.Join(errs...)
}
