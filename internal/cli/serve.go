// Package cli implements the promised-lan subcommand handlers. Each exported
// function returns a *cli.Command that main.go wires into the urfave app.
package cli

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/savaki/promised-lan/internal/config"
	"github.com/savaki/promised-lan/internal/nm"
	"github.com/savaki/promised-lan/internal/web"
)

func ServeCommand(logger *slog.Logger, configFlag cli.Flag) *cli.Command {
	return &cli.Command{
		Name:  "serve",
		Usage: "run the promised-lan daemon",
		Flags: []cli.Flag{configFlag},
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

			// Create profile at startup so it exists before any web request.
			startCtx, cancelStart := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancelStart()
			if err := client.EnsureProfile(startCtx); err != nil {
				logger.Error("ensure profile failed", "err", err)
				return err
			}

			srv := web.NewServer(cfg, client, logger)

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return srv.Run(ctx)
		},
	}
}
