package cli

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/savaki/promised-lan/internal/config"
	"github.com/savaki/promised-lan/internal/nm"
)

//go:embed promised-lan.service
var systemdUnitContent []byte

const (
	installedBinaryPath = "/usr/local/bin/promised-lan"
	configDir           = "/etc/promised-lan"
	configPath          = "/etc/promised-lan/config.yaml"
	unitPath            = "/etc/systemd/system/promised-lan.service"
)

func InstallCommand(logger *slog.Logger) *cli.Command {
	return &cli.Command{
		Name:  "install",
		Usage: "install promised-lan as a systemd service",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "upstream-interface", Value: "wlan0"},
			&cli.StringFlag{Name: "interior-interface", Value: "wlan1"},
			&cli.IntFlag{Name: "http-port", Value: 8080},
			&cli.BoolFlag{Name: "force", Usage: "overwrite existing config"},
		},
		Action: func(c *cli.Context) error {
			cfg := config.Config{
				UpstreamInterface: c.String("upstream-interface"),
				InteriorInterface: c.String("interior-interface"),
				HTTPPort:          c.Int("http-port"),
			}
			// Apply defaults so Validate() sees complete config.
			tmp := cfg
			applyDefaultsForInstall(&tmp)
			if err := tmp.Validate(); err != nil {
				return err
			}

			deps := DefaultPreflightDeps()
			if err := Preflight(tmp, deps); err != nil {
				return err
			}

			if err := copyBinary(); err != nil {
				return fmt.Errorf("copy binary: %w", err)
			}
			if err := writeConfig(tmp, c.Bool("force")); err != nil {
				return fmt.Errorf("write config: %w", err)
			}
			if err := writeUnit(); err != nil {
				return fmt.Errorf("write unit: %w", err)
			}
			if err := systemctl("daemon-reload"); err != nil {
				return fmt.Errorf("daemon-reload: %w", err)
			}
			if err := systemctl("enable", "--now", "promised-lan"); err != nil {
				return fmt.Errorf("enable: %w", err)
			}

			// Create the NM profile so the first web request doesn't have to.
			client := nm.NewNMCLI(nm.NMCLIConfig{
				UpstreamInterface: tmp.UpstreamInterface,
				ConnectionName:    tmp.ConnectionName,
				ConnectTimeout:    time.Duration(tmp.ConnectTimeoutSeconds) * time.Second,
				Logger:            logger,
			})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := client.EnsureProfile(ctx); err != nil {
				logger.Warn("could not ensure NM profile yet (daemon will retry)", "err", err)
			}

			fmt.Printf("installed. Web UI will be at http://<interior-ip>:%d once the interior interface is up.\n", tmp.HTTPPort)
			return nil
		},
	}
}

// applyDefaultsForInstall is a copy of config.Config.applyDefaults intended
// for use by install, since applyDefaults is unexported in the config package.
// Keep in sync with internal/config/config.go.
func applyDefaultsForInstall(c *config.Config) {
	if c.UpstreamInterface == "" {
		c.UpstreamInterface = "wlan0"
	}
	if c.InteriorInterface == "" {
		c.InteriorInterface = "wlan1"
	}
	if c.HTTPPort == 0 {
		c.HTTPPort = 8080
	}
	if c.ConnectionName == "" {
		c.ConnectionName = "promised-lan-upstream"
	}
	if c.ConnectTimeoutSeconds == 0 {
		c.ConnectTimeoutSeconds = 20
	}
}

func copyBinary() error {
	src, err := os.Executable()
	if err != nil {
		return err
	}
	if filepath.Clean(src) == filepath.Clean(installedBinaryPath) {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(installedBinaryPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Chmod(0755)
}

func writeConfig(cfg config.Config, force bool) error {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}
	if _, err := os.Stat(configPath); err == nil && !force {
		return nil // leave existing config alone
	}
	data, err := cfg.Marshal()
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

func writeUnit() error {
	return os.WriteFile(unitPath, systemdUnitContent, 0644)
}

func systemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
