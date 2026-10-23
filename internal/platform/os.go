package platform

import (
	"os"
	"runtime"
	"strings"
)

func OSName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	default:
		return runtime.GOOS
	}
}

func DeviceName() string {
	host, err := os.Hostname()
	if err != nil {
		return OSName()
	}
	return deviceNameOf(host, OSName())
}

func deviceNameOf(host, fallback string) string {
	name, _, _ := strings.Cut(strings.TrimSpace(host), ".")
	if name == "" {
		return fallback
	}
	return name
}
