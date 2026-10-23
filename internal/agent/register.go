package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type manifest struct {
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Path              string   `json:"path"`
	Type              string   `json:"type"`
	AllowedOrigins    []string `json:"allowed_origins,omitempty"`
	AllowedExtensions []string `json:"allowed_extensions,omitempty"`
}

func chromiumManifest(host string, ids browserIDs) manifest {
	return manifest{
		Name:           HostName,
		Description:    "Schemo: connects the browser extension to the app",
		Path:           host,
		Type:           "stdio",
		AllowedOrigins: ids.origins(),
	}
}

func firefoxManifest(host string, ids browserIDs) manifest {
	return manifest{
		Name:              HostName,
		Description:       "Schemo: connects the browser extension to the app",
		Path:              host,
		Type:              "stdio",
		AllowedExtensions: []string{ids.Firefox},
	}
}

// writeManifest rewrites the manifest on every start: the app may have moved.
func writeManifest(dir string, m manifest) error {
	path := filepath.Join(dir, HostName+".json")
	if len(m.AllowedOrigins)+len(m.AllowedExtensions) == 0 {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
