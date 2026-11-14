package har

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// A binary answer goes into the file as base64, so a reader gets the bytes back instead of U+FFFD.
func TestABinaryResponseIsWrittenAsBase64(t *testing.T) {
	entry := sample()
	entry.Record.ContentType = "image/png"
	entry.ResponseBody = "\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR"

	data, err := Export("Schemo", "2.4.0", []Entry{entry})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	var doc logFile
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	content := doc.Log.Entries[0].Response.Content
	decoded, err := base64.StdEncoding.DecodeString(content.Text)
	if content.Encoding != "base64" || err != nil || string(decoded) != entry.ResponseBody {
		t.Errorf("content = %+v, want the bytes as base64", content)
	}
}
