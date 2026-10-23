package request

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"strings"

	"schemo/internal/domain"
)

// FormatBody re-indents a body whose notation has a shape of its own, and returns the text it was
// given when it cannot: a document that does not parse is the person's to finish, and a formatter
// that rewrote half of it would make that harder.
//
// It lives here because reading a notation is Go's: re-typing JSON through the window's parser
// loses the digits of a number past 2^53 and writes `1.0` as `1`. Here the document is tokenised
// and every token is written back as the bytes it was read from.
func FormatBody(kind domain.BodyKind, text string, indent domain.Indent) string {
	var formatted string
	var ok bool
	switch kind {
	case domain.BodyJSON:
		formatted, ok = formatJSON(text, indent.Unit())
	case domain.BodyXML:
		formatted, ok = formatXML(text, indent.Unit())
	}
	if !ok {
		return text
	}
	return formatted
}

func formatJSON(text, unit string) (string, bool) {
	var out bytes.Buffer
	if err := json.Indent(&out, []byte(strings.TrimSpace(text)), "", unit); err != nil {
		return "", false
	}
	return out.String(), true
}

type xmlPiece struct {
	kind xmlKind
	raw  string
}

type xmlKind int

const (
	xmlOpen xmlKind = iota
	xmlClose
	xmlText
	// xmlStandalone is anything that stands on a line of its own and holds nothing: an empty element,
	// a comment, a declaration, a doctype.
	xmlStandalone
)

// formatXML puts every tag on a line of its own at the depth it opens at. An element that holds one
// piece of text and nothing else stays on one line, and whitespace between two tags goes.
func formatXML(text, unit string) (string, bool) {
	pieces, ok := xmlPieces(text)
	if !ok {
		return "", false
	}

	var lines []string
	depth := 0
	line := func(s string) { lines = append(lines, strings.Repeat(unit, depth)+s) }
	for i := 0; i < len(pieces); i++ {
		piece := pieces[i]
		switch piece.kind {
		case xmlOpen:
			if i+2 < len(pieces) && pieces[i+1].kind == xmlText &&
				strings.TrimSpace(pieces[i+1].raw) != "" && pieces[i+2].kind == xmlClose {
				line(piece.raw + pieces[i+1].raw + pieces[i+2].raw)
				i += 2
				continue
			}
			line(piece.raw)
			depth++
		case xmlClose:
			depth--
			line(piece.raw)
		case xmlText:
			if trimmed := strings.TrimSpace(piece.raw); trimmed != "" {
				line(trimmed)
			}
		default:
			line(piece.raw)
		}
	}
	return strings.Join(lines, "\n"), true
}

// xmlPieces cuts a document into the pieces it is made of, each as the bytes it came from. The
// decoder supplies the structure and is checked for balance; a piece's text is read off the source
// by position, which keeps a prefix, a quote and a CDATA marker as written.
func xmlPieces(text string) ([]xmlPiece, bool) {
	decoder := xml.NewDecoder(strings.NewReader(text))
	// A declared encoding is the document's own business; the text arrives already decoded.
	decoder.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) { return in, nil }

	var pieces []xmlPiece
	selfClosed := false
	for {
		before := decoder.InputOffset()
		token, err := decoder.Token()
		if err == io.EOF {
			return pieces, len(pieces) > 0
		}
		if err != nil {
			return nil, false
		}
		raw := text[before:decoder.InputOffset()]

		switch token.(type) {
		case xml.StartElement:
			if strings.HasSuffix(raw, "/>") {
				selfClosed = true
				pieces = append(pieces, xmlPiece{xmlStandalone, raw})
			} else {
				pieces = append(pieces, xmlPiece{xmlOpen, raw})
			}
		case xml.EndElement:
			// The decoder answers an empty element with a start and an end; the end has no bytes.
			if selfClosed {
				selfClosed = false
				continue
			}
			pieces = append(pieces, xmlPiece{xmlClose, raw})
		case xml.CharData:
			pieces = append(pieces, xmlPiece{xmlText, raw})
		default:
			pieces = append(pieces, xmlPiece{xmlStandalone, raw})
		}
	}
}
