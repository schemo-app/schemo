package settings

import (
	"context"
	"encoding/json"
	"strings"

	"schemo/internal/domain"
)

func (u *UseCase) SetRequests(ctx context.Context, patch RequestsPatch) (domain.Settings, error) {
	held, err := u.network(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	sent := held.Requests
	if patch.Timeout != nil {
		sent.Timeout = clampTimeout(*patch.Timeout)
	}
	if patch.FollowRedirects != nil {
		sent.FollowRedirects = *patch.FollowRedirects
	}
	if patch.MaxRedirects != nil {
		sent.MaxRedirects = clampRedirects(*patch.MaxRedirects)
	}
	if patch.Cookies != nil {
		sent.Cookies = *patch.Cookies
	}
	if patch.HTTPVersion != nil {
		sent.HTTPVersion = *patch.HTTPVersion
	}
	if patch.UserAgent != nil {
		sent.UserAgent = strings.TrimSpace(*patch.UserAgent)
	}
	if patch.AcceptEncoding != nil {
		sent.AcceptEncoding = *patch.AcceptEncoding
	}
	if patch.ShowAuto != nil {
		sent.ShowAuto = *patch.ShowAuto
	}
	if err := sent.Valid(); err != nil {
		return domain.Settings{}, err
	}
	if err := u.saveBlob(ctx, domain.SettingRequests, sent); err != nil {
		return domain.Settings{}, err
	}
	held.Requests = sent
	u.outbound.Apply(held)
	return u.announced(u.Snapshot(ctx))
}

func (u *UseCase) SetCertificates(
	ctx context.Context,
	patch CertificatesPatch,
) (domain.Settings, error) {
	held, err := u.network(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	certs := held.Certificates
	if patch.Verify != nil {
		certs.Verify = *patch.Verify
	}
	if patch.TrustLocalhost != nil {
		certs.TrustLocalhost = *patch.TrustLocalhost
	}
	if err := u.saveBlob(ctx, domain.SettingCertificates, certs); err != nil {
		return domain.Settings{}, err
	}
	held.Certificates = certs
	u.outbound.Apply(held)
	return u.announced(u.Snapshot(ctx))
}

func (u *UseCase) Restore(ctx context.Context) error {
	held, err := u.network(ctx)
	if err != nil {
		return err
	}
	u.outbound.Apply(held)
	return nil
}

func (u *UseCase) network(ctx context.Context) (domain.Network, error) {
	stored, err := u.store.Settings(ctx)
	if err != nil {
		return domain.Network{}, err
	}
	return domain.Network{
		Proxy:        parseProxy(stored[domain.SettingProxy]),
		Certificates: parseCertificates(stored[domain.SettingCertificates]),
		Requests:     parseRequests(stored[domain.SettingRequests]),
	}, nil
}

func (u *UseCase) saveBlob(ctx context.Context, key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return u.save(ctx, key, string(encoded))
}

func parseRequests(raw string) domain.Requests {
	out := domain.DefaultRequests()
	if raw == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return domain.DefaultRequests()
	}
	if !out.HTTPVersion.Valid() {
		out.HTTPVersion = domain.HTTPAuto
	}
	out.Timeout = clampTimeout(out.Timeout)
	out.MaxRedirects = clampRedirects(out.MaxRedirects)
	return out
}

func parseCertificates(raw string) domain.Certificates {
	out := domain.DefaultCertificates()
	if raw == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return domain.DefaultCertificates()
	}
	out.Roots = rootsWithContents(out.Roots)
	return out
}

func rootsWithContents(roots []domain.Root) []domain.Root {
	kept := make([]domain.Root, 0, len(roots))
	for _, root := range roots {
		if strings.TrimSpace(root.PEM) != "" {
			kept = append(kept, root)
		}
	}
	return kept
}

func clampTimeout(seconds int) int {
	return min(max(seconds, 0), domain.MaxRequestTimeout)
}

func clampRedirects(count int) int {
	return min(max(count, 0), domain.MaxRequestRedirect)
}
