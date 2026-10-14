package main

import (
	"context"
	"embed"
	"log"
	"os"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"go.uber.org/fx"

	"schemo/internal/platform"
	"schemo/internal/transport/desktop"
	"schemo/internal/transport/extension"
	"schemo/internal/transport/wails"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

const lifecycleTimeout = 5 * time.Second

func main() {
	updater.HandleHelperMode()

	if extension.Launched(os.Args[1:]) {
		os.Exit(relay())
	}

	os.Exit(run())
}

func relay() int {
	dir, err := platform.NewDataDir()
	if err != nil {
		log.Printf("[relay] data dir: %v", err)
		return 1
	}

	if err := extension.Relay(os.Stdin, os.Stdout, dir.BridgeSocketPath()); err != nil {
		log.Printf("[relay] %v", err)
		return 1
	}

	return 0
}

func run() int {
	var app *application.App

	graph := fx.New(
		fx.NopLogger,
		wails.Module,
		fx.Supply(desktop.Assets{
			FS:   assets,
			Icon: appIcon,
		}),
		fx.Populate(&app),
	)

	if err := graph.Err(); err != nil {
		log.Printf("[app] build: %v", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), lifecycleTimeout)
	defer cancel()

	if err := graph.Start(ctx); err != nil {
		log.Printf("[app] startup: %v", err)
		return 1
	}

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), lifecycleTimeout)
		defer cancel()

		if err := graph.Stop(ctx); err != nil {
			log.Printf("[app] shutdown: %v", err)
		}
	}()

	if err := app.Run(); err != nil {
		log.Printf("[app] run: %v", err)
		return 1
	}

	return 0
}
