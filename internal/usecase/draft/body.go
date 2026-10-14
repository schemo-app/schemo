package draft

import (
	"bytes"
	"fmt"
	"mime"
	"mime/multipart"
	"net/textproto"
	"path/filepath"
	"strings"

	"schemo/internal/domain"
)

// renderBody is one body as the text that goes on the wire. `used` is the boundary the form wrote,
// returned so the header naming it cannot disagree with the body. A boundary handed in is reused:
// the live and the masked copies are then two renderings of one encoding.
func (u *UseCase) renderBody(
	kind domain.BodyKind,
	text string,
	file string,
	rows []domain.FormRow,
	boundary string,
	masked bool,
) (body string, used string, err error) {
	switch kind {
	case domain.BodyForm:
		return u.renderForm(rows, boundary, masked)
	case domain.BodyBinary:
		// The path, not the text: a binary body is the file, and the box beside it holds what was typed.
		return u.renderFile(file, masked)
	default:
		return text, "", nil
	}
}

// Only rows that are switched on and named go out: a blank key is a part no server can read.
func (u *UseCase) renderForm(
	rows []domain.FormRow,
	boundary string,
	masked bool,
) (string, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if boundary != "" {
		// An invalid boundary can only come from here, so a failure is a bug rather than a state.
		if err := writer.SetBoundary(boundary); err != nil {
			return "", "", fmt.Errorf("form boundary: %w", err)
		}
	}
	used := writer.Boundary()

	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if !row.Enabled || name == "" {
			continue
		}
		if row.File {
			if err := writeFormFile(writer, row, u, masked); err != nil {
				return "", "", err
			}
			continue
		}
		if err := writer.WriteField(name, row.Value); err != nil {
			return "", "", fmt.Errorf("form field %q: %w", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		return "", "", fmt.Errorf("closing the form: %w", err)
	}
	return buf.String(), used, nil
}

// CreatePart rather than CreateFormFile: the latter hardcodes application/octet-stream.
func writeFormFile(writer *multipart.Writer, row domain.FormRow, u *UseCase, masked bool) error {
	name := strings.TrimSpace(row.Name)
	file := row.Src

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`,
		escapeQuotes(name), escapeQuotes(filepath.Base(file))))
	header.Set("Content-Type", typeOfFile(file))

	part, err := writer.CreatePart(header)
	if err != nil {
		return fmt.Errorf("form part %q: %w", name, err)
	}
	// A masked part names the file and carries nothing: a record is a description, not the upload.
	if masked {
		return nil
	}
	data, err := u.files.Read(file)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return fmt.Errorf("writing %s: %w", file, err)
	}
	return nil
}

// The masked copy is empty for the reason a masked part is.
func (u *UseCase) renderFile(path string, masked bool) (string, string, error) {
	if masked || path == "" {
		return "", "", nil
	}
	data, err := u.files.Read(path)
	if err != nil {
		return "", "", err
	}
	return string(data), "", nil
}

// The extension is all there is to go on without opening the file, and it is what every client
// uses; an unknown one still declares the bytes rather than nothing at all.
func typeOfFile(path string) string {
	if guessed := mime.TypeByExtension(filepath.Ext(path)); guessed != "" {
		return guessed
	}
	return "application/octet-stream"
}

// multipart's escaping for a filename in a header: a quote would otherwise end the parameter.
var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func escapeQuotes(s string) string {
	return quoteEscaper.Replace(s)
}
