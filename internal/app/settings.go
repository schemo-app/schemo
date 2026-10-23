package app

import (
	"context"
	"log"
	"reflect"

	"schemo/internal/agent"
	"schemo/internal/desktop"
	"schemo/internal/domain"
	"schemo/internal/request"
	"schemo/internal/workspace"
)

// watchSettings connects saved settings to what carries them out. The MCP server goes first: it is
// the one that can refuse (a port that cannot be taken), and a refusal undoes the save.
func watchSettings(
	settings *workspace.Settings,
	net outbound,
	host *desktop.Host,
	server *agent.MCP,
	bridge *agent.Extension,
	history *request.History,
) {
	settings.Watch(func(_ context.Context, before, after domain.Settings) error {
		if reflect.DeepEqual(before.MCP, after.MCP) {
			return nil
		}
		if err := server.Apply(after.MCP); err != nil {
			return err
		}
		if before.MCP.Token != "" && before.MCP.Token != after.MCP.Token {
			// A new token ends every agent that was speaking through the old one.
			server.Rotate(after.MCP.Token)
		}
		return nil
	})
	settings.Watch(func(ctx context.Context, before, after domain.Settings) error {
		if !reflect.DeepEqual(before.Network(), after.Network()) {
			net.Apply(after.Network())
		}
		if before.Theme != after.Theme {
			host.SetTheme(after.Theme)
		}
		if before.Language != after.Language {
			host.SetLanguage(after.Language)
		}
		if before.Appearance != after.Appearance {
			host.SetAppearance(after.Appearance)
		}
		if !reflect.DeepEqual(before.CaptureFilters, after.CaptureFilters) {
			logFailure("handing capture filters to the extension", bridge.ApplyCaptureFilters(after.CaptureFilters))
		}
		if before.Extension != after.Extension {
			logFailure("handing settings to the extension", bridge.ApplyExtensionSettings(after.Extension))
		}
		if before.HistoryRetention != after.HistoryRetention {
			// The rule is saved either way; the next prune applies it.
			_, err := history.Prune(ctx)
			logFailure("pruning history", err)
		}
		return nil
	})
}

func logFailure(operation string, err error) {
	if err != nil {
		log.Printf("[settings] %s: %v", operation, err)
	}
}
