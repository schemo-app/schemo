package main

import (
	"embed"
	"log"
	"os"

	"github.com/wailsapp/wails/v3/pkg/updater"

	"schemo/internal/agent"
	"schemo/internal/app"
	"schemo/internal/desktop"
	"schemo/internal/platform"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

func main() {
	updater.HandleHelperMode()

	// The browser extension starts this binary as its native-messaging host.
	if agent.Launched(os.Args[1:]) {
		os.Exit(relay())
	}

	if err := app.Run(desktop.Assets{FS: assets, Icon: appIcon}); err != nil {
		log.Printf("[app] %v", err)
		os.Exit(1)
	}
}

func relay() int {
	dir, err := platform.NewDataDir()
	if err != nil {
		log.Printf("[relay] data dir: %v", err)
		return 1
	}
	if err := agent.Relay(os.Stdin, os.Stdout, dir.BridgeSocketPath()); err != nil {
		log.Printf("[relay] %v", err)
		return 1
	}
	return 0
}
