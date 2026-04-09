package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/savaki/promised-lan/internal/config"
	"github.com/savaki/promised-lan/internal/nm"
)

func ScanCommand(logger *slog.Logger, configFlag cli.Flag) *cli.Command {
	return &cli.Command{
		Name:  "scan",
		Usage: "list nearby networks on the upstream radio",
		Flags: []cli.Flag{
			configFlag,
			&cli.BoolFlag{Name: "json", Usage: "emit JSON"},
		},
		Action: func(c *cli.Context) error {
			cfg, err := config.Load(c.String("config"))
			if err != nil {
				return err
			}
			client := nm.NewNMCLI(nm.NMCLIConfig{
				UpstreamInterface: cfg.UpstreamInterface,
				ConnectionName:    cfg.ConnectionName,
				ConnectTimeout:    time.Duration(cfg.ConnectTimeoutSeconds) * time.Second,
				Logger:            logger,
			})
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			nets, err := client.Scan(ctx)
			if err != nil {
				return err
			}
			if c.Bool("json") {
				return json.NewEncoder(os.Stdout).Encode(nets)
			}
			fmt.Printf("%-32s %-8s %-6s %s\n", "SSID", "SECURITY", "SIGNAL", "CURRENT")
			for _, n := range nets {
				cur := ""
				if n.Current {
					cur = "*"
				}
				fmt.Printf("%-32s %-8s %d/5    %s\n", n.SSID, n.Sec, n.Signal, cur)
			}
			return nil
		},
	}
}
