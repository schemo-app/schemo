package desktop

import (
	"context"
	"errors"

	wailsupdater "github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/endpoint"

	"schemo/internal/domain"
)

// source requires signed releases from the Wails endpoint provider.
// Engine serializes channel changes with checks and installations.
type source struct {
	wailsupdater.Provider
	url string
}

func newSource(url string) (*source, error) {
	s := &source{url: url}
	return s, s.use(domain.ChannelStable)
}

func (s *source) use(channel domain.UpdateChannel) error {
	if !channel.Valid() {
		return errors.New("updater: invalid release channel")
	}
	p, err := endpoint.New(endpoint.Config{URL: s.url, Channel: string(channel)})
	if err != nil {
		return err
	}
	s.Provider = p
	return nil
}

func (s *source) Check(
	ctx context.Context,
	req wailsupdater.CheckRequest,
) (*wailsupdater.Release, error) {
	release, err := s.Provider.Check(ctx, req)
	if err != nil || release == nil {
		return nil, err
	}
	if release.Verification == nil || len(release.Verification.Signature) == 0 {
		return nil, errors.New("release " + release.Version + " is not signed")
	}
	return release, nil
}
