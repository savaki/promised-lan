package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/urfave/cli/v2"
	"golang.org/x/term"

	"github.com/savaki/promised-lan/internal/config"
	"github.com/savaki/promised-lan/internal/nm"
)

func SetCommand(logger *slog.Logger, configFlag cli.Flag) *cli.Command {
	return &cli.Command{
		Name:  "set",
		Usage: "update upstream credentials from the shell",
		Flags: []cli.Flag{
			configFlag,
			&cli.StringFlag{Name: "ssid", Usage: "SSID", Required: true},
			&cli.StringFlag{Name: "psk", Usage: "passphrase (use - to prompt)", Value: "-"},
		},
		Action: func(c *cli.Context) error {
			cfg, err := config.Load(c.String("config"))
			if err != nil {
				return err
			}
			psk := c.String("psk")
			if psk == "-" {
				if !term.IsTerminal(int(os.Stdin.Fd())) {
					return fmt.Errorf("--psk - requires a terminal")
				}
				fmt.Fprint(os.Stderr, "passphrase: ")
				b, err := term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Fprintln(os.Stderr)
				if err != nil {
					return err
				}
				psk = string(b)
			}
			if len(psk) < 8 || len(psk) > 63 {
				return fmt.Errorf("passphrase must be 8..63 bytes")
			}

			client := nm.NewNMCLI(nm.NMCLIConfig{
				UpstreamInterface: cfg.UpstreamInterface,
				ConnectionName:    cfg.ConnectionName,
				ConnectTimeout:    time.Duration(cfg.ConnectTimeoutSeconds) * time.Second,
				Logger:            logger,
			})

			ctx, cancel := context.WithTimeout(context.Background(),
				time.Duration(cfg.ConnectTimeoutSeconds+10)*time.Second)
			defer cancel()

			st, err := client.UpdateAndActivate(ctx, c.String("ssid"), psk)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed: %v\n", err)
				return err
			}
			fmt.Printf("connected: %s (%s)\n", st.SSID, st.IPv4)
			return nil
		},
	}
}
