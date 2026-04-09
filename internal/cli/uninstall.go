package cli

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/urfave/cli/v2"
)

func UninstallCommand(logger *slog.Logger) *cli.Command {
	return &cli.Command{
		Name:  "uninstall",
		Usage: "remove promised-lan (binary, service, and optionally config/profile)",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "purge", Usage: "also remove config and NM profile"},
		},
		Action: func(c *cli.Context) error {
			if os.Geteuid() != 0 {
				return errors.New("must run as root")
			}
			var errs []error
			if err := systemctl("disable", "--now", "promised-lan"); err != nil {
				logger.Warn("systemctl disable failed (continuing)", "err", err)
			}
			if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
			if err := systemctl("daemon-reload"); err != nil {
				errs = append(errs, err)
			}
			if err := os.Remove(installedBinaryPath); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
			if c.Bool("purge") {
				if err := os.RemoveAll(configDir); err != nil {
					errs = append(errs, err)
				}
				// Best-effort: delete the NM profile. Don't fail if nmcli is missing.
				_ = exec.Command("nmcli", "con", "delete", "promised-lan-upstream").Run()
			}
			if len(errs) > 0 {
				return errors.Join(errs...)
			}
			fmt.Println("uninstalled.")
			return nil
		},
	}
}
