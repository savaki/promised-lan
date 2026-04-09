package main

import (
	"log/slog"
	"os"

	"github.com/urfave/cli/v2"

	pcli "github.com/savaki/promised-lan/internal/cli"
)

const defaultConfigPath = "/etc/promised-lan/config.yaml"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	configFlag := &cli.StringFlag{
		Name:  "config",
		Usage: "path to config file",
		Value: defaultConfigPath,
	}

	app := &cli.App{
		Name:  "promised-lan",
		Usage: "manage the upstream WiFi link on a Raspberry Pi",
		Commands: []*cli.Command{
			pcli.ServeCommand(logger, configFlag),
			pcli.StatusCommand(logger, configFlag),
			pcli.ScanCommand(logger, configFlag),
			pcli.SetCommand(logger, configFlag),
			pcli.InstallCommand(logger),
			pcli.UninstallCommand(logger),
		},
	}

	if err := app.Run(os.Args); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}
