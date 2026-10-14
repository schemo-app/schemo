package settings

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"strings"

	"schemo/internal/domain"
)

func (u *UseCase) AddRootCertificate(
	ctx context.Context,
	name string,
	contents string,
) (domain.Settings, error) {
	shown, ok := rootName(name, contents)
	if !ok {
		return domain.Settings{}, domain.Refuse(domain.CodeBadCertificate, domain.ErrNotAllowed,
			domain.Args{"name": name})
	}

	held, err := u.network(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	roots := append(withoutRoot(held.Certificates.Roots, shown, contents),
		domain.Root{Name: shown, PEM: contents})
	return u.saveRoots(ctx, held, roots)
}

func (u *UseCase) RemoveRootCertificate(ctx context.Context, name string) (domain.Settings, error) {
	held, err := u.network(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	kept := make([]domain.Root, 0, len(held.Certificates.Roots))
	for _, root := range held.Certificates.Roots {
		if root.Name != name {
			kept = append(kept, root)
		}
	}
	return u.saveRoots(ctx, held, kept)
}

func (u *UseCase) saveRoots(
	ctx context.Context,
	held domain.Network,
	roots []domain.Root,
) (domain.Settings, error) {
	held.Certificates.Roots = roots
	if err := u.saveBlob(ctx, domain.SettingCertificates, held.Certificates); err != nil {
		return domain.Settings{}, err
	}
	u.outbound.Apply(held)
	return u.announced(u.Snapshot(ctx))
}

func rootName(name, contents string) (string, bool) {
	for rest := []byte(contents); ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return "", false
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		parsed, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		if common := strings.TrimSpace(parsed.Subject.CommonName); common != "" {
			return common, true
		}
		return strings.TrimSpace(name), true
	}
}

func withoutRoot(roots []domain.Root, name, contents string) []domain.Root {
	kept := make([]domain.Root, 0, len(roots)+1)
	for _, root := range roots {
		if root.Name == name || root.PEM == contents {
			continue
		}
		kept = append(kept, root)
	}
	return kept
}
