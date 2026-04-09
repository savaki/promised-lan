package cli

import (
	"errors"
	"log/slog"

	"github.com/urfave/cli/v2"
)

// InstallCommand is a stub replaced by the real implementation in install.go (Phase E).
func InstallCommand(logger *slog.Logger) *cli.Command {
	return &cli.Command{
		Name:  "install",
		Usage: "(stub) install promised-lan as a systemd service",
		Action: func(c *cli.Context) error {
			return errors.New("install not yet implemented")
		},
	}
}

// UninstallCommand is a stub replaced by the real implementation in uninstall.go (Phase E).
func UninstallCommand(logger *slog.Logger) *cli.Command {
	return &cli.Command{
		Name:  "uninstall",
		Usage: "(stub) remove promised-lan",
		Action: func(c *cli.Context) error {
			return errors.New("uninstall not yet implemented")
		},
	}
}
