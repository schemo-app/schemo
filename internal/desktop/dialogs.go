package desktop

import (
	"errors"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The file and browser dialogs. Every one of them is answered with "nothing" when the person
// closes it: a cancelled dialog is not a failure the window has to explain.

// OpenURL opens the system browser, which sign-in pages need for a real address bar and cookies.
func (h *Host) OpenURL(rawURL string) error {
	app := h.App()
	if app == nil {
		return errors.New("the window does not exist yet")
	}
	return app.Browser.OpenURL(rawURL)
}

// SafeFileName replaces characters a filesystem refuses with a dash and adds the extension. An
// empty result falls back to `fallback`.
func SafeFileName(name, fallback, extension string) string {
	safe := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, strings.TrimSpace(name))
	if safe == "" {
		safe = fallback
	}
	return safe + extension
}

// OpenFile returns the chosen path, or "" when the dialog was closed.
func (h *Host) OpenFile(title string, filters ...application.FileFilter) (string, error) {
	app := h.App()
	if app == nil {
		return "", errors.New("the window does not exist yet")
	}
	dialog := app.Dialog.OpenFile().SetTitle(title)
	for _, filter := range filters {
		dialog = dialog.AddFilter(filter.DisplayName, filter.Pattern)
	}
	path, err := dialog.PromptForSingleSelection()
	return path, withoutCancellation(err)
}

// OpenFolder returns the chosen folder, or nothing when the dialog was closed.
func (h *Host) OpenFolder(title string) (string, error) {
	app := h.App()
	if app == nil {
		return "", errors.New("the window does not exist yet")
	}
	path, err := app.Dialog.OpenFile().SetTitle(title).CanChooseDirectories(true).CanChooseFiles(false).
		PromptForSingleSelection()
	return path, withoutCancellation(err)
}

// withoutCancellation turns a closed dialog into no error; Wails' sentinel is internal, so the text
// is matched.
func withoutCancellation(err error) error {
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "cancel") {
		return nil
	}
	return err
}

// SaveFile answers with the path the user chose, or with nothing when they chose none.
func (h *Host) SaveFile(
	title string,
	suggestedName string,
	filters ...application.FileFilter,
) (string, error) {
	app := h.App()
	if app == nil {
		return "", errors.New("the window does not exist yet")
	}
	path, err := app.Dialog.SaveFileWithOptions(&application.SaveFileDialogOptions{
		Title:   title,
		Message: title,
		// Filename is only a suggestion: the user types over it or picks another place.
		Filename: suggestedName,
		Filters:  filters,
	}).PromptForSingleSelection()
	return path, withoutCancellation(err)
}

// PickRootCertificate opens a certificate file; "" when the dialog was closed.
func (h *Host) PickRootCertificate(title string) (string, error) {
	return h.OpenFile(title, application.FileFilter{
		DisplayName: title, Pattern: "*.pem;*.crt;*.cer",
	})
}
