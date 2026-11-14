package request

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"schemo/internal/desktop"
	"schemo/internal/domain"
)

const responseHexBytes = 256

// BinaryPreview is the prefix and optional playable media of a stored response.
type BinaryPreview struct {
	ContentType string `json:"contentType"`
	Hex         string `json:"hex"`
	HexBytes    int    `json:"hexBytes"`
	MediaURL    string `json:"mediaURL,omitempty"`
}

// ResponsePreview reads the first 256 bytes and includes the full payload only for images or video.
func (s *History) ResponsePreview(ctx context.Context, id string) (BinaryPreview, error) {
	record, err := s.Record(ctx, id)
	if err != nil {
		return BinaryPreview{}, err
	}
	content, err := s.content(ctx, id, domain.SideResponse)
	if err != nil {
		return BinaryPreview{}, err
	}

	prefix := content[:min(len(content), responseHexBytes)]
	preview := BinaryPreview{
		ContentType: previewType(record.ContentType, content),
		Hex:         hex.Dump([]byte(prefix)),
		HexBytes:    len(prefix),
	}
	if content != "" && (strings.HasPrefix(preview.ContentType, "image/") || strings.HasPrefix(preview.ContentType, "video/")) {
		preview.MediaURL = "data:" + preview.ContentType + ";base64," + base64.StdEncoding.EncodeToString([]byte(content))
	}
	return preview, nil
}

// SaveResponseBody writes a record's response to a file the person picks.
func (s *History) SaveResponseBody(ctx context.Context, title, id string) (bool, error) {
	record, err := s.Record(ctx, id)
	if err != nil {
		return false, err
	}
	content, err := s.content(ctx, id, domain.SideResponse)
	if err != nil {
		return false, err
	}
	path, err := s.host.SaveFile(title, responseFileName(record))
	if err != nil || path == "" {
		return false, err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return false, fmt.Errorf("file %s: %w", path, err)
	}
	return true, nil
}

func previewType(declared, content string) string {
	media, _, _ := mime.ParseMediaType(declared)
	sniffed := http.DetectContentType([]byte(content[:min(len(content), 512)]))
	// Some servers send images and video as application/octet-stream.
	if strings.HasPrefix(sniffed, "image/") || media == "" || media == "application/octet-stream" {
		return sniffed
	}
	return media
}

// responseFileName is the name the server gave the file, else the one its address ends in, else
// "response" with the extension its media type is known by.
func responseFileName(record domain.Record) string {
	for _, header := range record.ResponseHeaders {
		if !strings.EqualFold(header.Name, "Content-Disposition") {
			continue
		}
		if _, params, err := mime.ParseMediaType(header.Value); err == nil && params["filename"] != "" {
			return desktop.SafeFileName(path.Base(params["filename"]), "response", "")
		}
	}
	if address, err := url.Parse(record.URL); err == nil {
		if name := path.Base(address.Path); path.Ext(name) != "" {
			return desktop.SafeFileName(name, "response", "")
		}
	}
	return desktop.SafeFileName("response", "response", extensionOf(record.ContentType))
}

func extensionOf(contentType string) string {
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	if extensions, _ := mime.ExtensionsByType(media); len(extensions) > 0 {
		return extensions[0]
	}
	return ""
}
