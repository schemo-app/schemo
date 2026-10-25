package convert

import (
	"os"
	"path/filepath"
	"testing"

	"schemo/internal/platform"
)

func TestPreviewPathStagesDroppedFileForImport(t *testing.T) {
	h := newHarness()
	h.transfer.files = platform.NewFileReader()
	path := filepath.Join(t.TempDir(), "ledger.postman.json")
	if err := os.WriteFile(path, []byte(postmanFile), 0o600); err != nil {
		t.Fatal(err)
	}

	preview, err := h.transfer.PreviewPath(t.Context(), "postman", path)
	if err != nil {
		t.Fatal(err)
	}
	if preview.File != "ledger.postman.json" || preview.Name != "Ledger" || preview.Requests != 3 {
		t.Fatalf("PreviewPath = %+v, want ledger.postman.json containing Ledger and 3 requests", preview)
	}
	if len(h.collections.imported) != 0 {
		t.Fatal("PreviewPath imported before confirmation")
	}
	summary, err := h.transfer.Apply(t.Context(), preview.Token, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Requests != 3 || len(h.collections.imported) != 1 {
		t.Fatalf("Apply = %+v, want 3 requests in one imported collection", summary)
	}
}

func TestPreviewPathRejectsUnreadableOrInvalidFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(path, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		format string
		path   string
		limit  int64
	}{
		{name: "unknown format", format: "unknown", path: path},
		{name: "missing file", format: "postman", path: path + ".missing"},
		{name: "invalid content", format: "postman", path: path},
		{name: "size limit", format: "postman", path: path, limit: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness()
			h.transfer.files = &platform.FileReader{MaxBytes: test.limit}
			if _, err := h.transfer.PreviewPath(t.Context(), test.format, test.path); err == nil {
				t.Fatal("PreviewPath succeeded, want an error")
			}
		})
	}
}
