package wails

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"schemo/internal/transport/desktop"
	"schemo/internal/usecase/transfer"
)

// TransferService is the boundary of what comes in from other tools and goes out to them: the
// dialogs a window cannot open for itself, and the writing of an exported file. The reading,
// counting and composing are the transfer use case's.
type TransferService struct {
	transfer *transfer.UseCase
	host     *desktop.Host
}

func NewTransferService(uc *transfer.UseCase, host *desktop.Host) *TransferService {
	return &TransferService{transfer: uc, host: host}
}

// PreviewFile asks for a file and reads it without writing anything: the dialog shows what the file
// holds before the person commits. A nil preview is a cancelled dialog, not a failure.
func (s *TransferService) PreviewFile(
	ctx context.Context,
	title string,
	format string,
) (*transfer.Preview, error) {
	parsed, err := transfer.FormatOf(format)
	if err != nil {
		return nil, err
	}
	path, err := s.host.OpenFile(title, filterOf(parsed)...)
	if err != nil || path == "" {
		return nil, err
	}
	preview, err := s.transfer.PreviewFile(ctx, parsed, path)
	if err != nil {
		return nil, err
	}
	preview.File = filepath.Base(path)
	return &preview, nil
}

// PreviewText reads a text the window already holds — a dropped file's contents, or a paste of
// commands — and names it the way the card will draw it.
func (s *TransferService) PreviewText(
	ctx context.Context,
	format string,
	text string,
	fileName string,
) (transfer.Preview, error) {
	parsed, err := transfer.FormatOf(format)
	if err != nil {
		return transfer.Preview{}, err
	}
	preview, err := s.transfer.PreviewText(ctx, parsed, text)
	if err != nil {
		return transfer.Preview{}, err
	}
	preview.File = fileName
	return preview, nil
}

// PreviewUrl reads a document from a URL — a specification or a collection somebody published.
func (s *TransferService) PreviewUrl(
	ctx context.Context,
	format string,
	url string,
) (transfer.Preview, error) {
	parsed, err := transfer.FormatOf(format)
	if err != nil {
		return transfer.Preview{}, err
	}
	preview, err := s.transfer.PreviewUrl(ctx, parsed, url)
	if err != nil {
		return transfer.Preview{}, err
	}
	preview.File = url
	return preview, nil
}

// TakeImportLink reports and clears the page a followed link named. Destructive on purpose: the
// window's mount and its event handler both call it, and only the first may open the dialog — the
// link is usually what started this app, so the mount is the one that usually finds it.
func (s *TransferService) TakeImportLink() string {
	return s.host.TakeImportLink()
}

// Apply writes what a preview read — the whole slice in one call, because it came from one document
// — and answers with the tree and the environment list as they are now.
func (s *TransferService) Apply(
	ctx context.Context,
	token string,
	parentID string,
	name string,
) (transfer.Summary, error) {
	return s.transfer.Apply(ctx, token, parentID, name)
}

// Spaces lists every space with the collections a copy could be taken from, with the size of each.
func (s *TransferService) Spaces(ctx context.Context) ([]transfer.Space, error) {
	return s.transfer.Spaces(ctx)
}

// ImportFromSpace copies the chosen collections of another space into the active one.
func (s *TransferService) ImportFromSpace(
	ctx context.Context,
	workspace string,
	ids []string,
	parentID string,
) (transfer.Summary, error) {
	return s.transfer.ImportFromSpace(ctx, workspace, ids, parentID)
}

// Export writes a collection into a file the user picks, the format's own ending on the suggested
// name, and answers with the name the file was written under. An empty name is a cancelled dialog,
// not an error.
func (s *TransferService) Export(
	ctx context.Context,
	title string,
	id string,
	format string,
	secrets bool,
) (string, error) {
	parsed, err := transfer.FormatOf(format)
	if err != nil {
		return "", err
	}
	out, err := s.transfer.Export(ctx, id, parsed, secrets)
	if err != nil {
		return "", err
	}

	kind := kindOf(parsed)
	path, err := s.host.SaveFile(title,
		desktop.SafeFileName(out.Name, "collection", kind.suffix), filterOf(parsed)...)
	if err != nil || path == "" {
		return "", err
	}
	// 0644 and not the database's 0600: the file is meant to be handed to somebody else. When the
	// person asked for the secret values, they are in it — and the window has said so.
	if err := os.WriteFile(path, out.Data, 0o644); err != nil {
		return "", fmt.Errorf("file %s: %w", path, err)
	}
	return filepath.Base(path), nil
}

// ExportText is the same export as the text the window copies: a collection somebody is handed in a
// chat is handed as its text.
func (s *TransferService) ExportText(
	ctx context.Context,
	id string,
	format string,
	secrets bool,
) (string, error) {
	parsed, err := transfer.FormatOf(format)
	if err != nil {
		return "", err
	}
	out, err := s.transfer.Export(ctx, id, parsed, secrets)
	if err != nil {
		return "", err
	}
	return string(out.Data), nil
}

// Curl writes a collection out as curl commands, one per request, each under a `#` line naming it:
// what the window copies is what the same paste reads back in.
func (s *TransferService) Curl(ctx context.Context, id string, secrets bool) (string, error) {
	return s.transfer.Curl(ctx, id, secrets)
}
