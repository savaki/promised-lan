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

func StatusCommand(logger *slog.Logger, configFlag cli.Flag) *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "print current upstream status",
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
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			st, err := client.Status(ctx)
			if err != nil {
				return err
			}
			if c.Bool("json") {
				return json.NewEncoder(os.Stdout).Encode(st)
			}
			fmt.Printf("link:      %s\n", st.Link)
			fmt.Printf("interface: %s\n", st.Interface)
			fmt.Printf("ssid:      %s\n", st.SSID)
			fmt.Printf("state:     %s\n", st.State)
			fmt.Printf("ipv4:      %s\n", st.IPv4)
			fmt.Printf("gateway:   %s\n", st.Gateway)
			fmt.Printf("signal:    %d/5\n", st.Signal)
			if st.Link != "up" {
				os.Exit(1)
			}
			return nil
		},
	}
}
