package publication

import (
	"bytes"
	"strings"

	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// descriptionProse derives public sections from top-level H2 headings. The collection keeps
// the complete Markdown document; code blocks, quotes and nested headings are not section titles.
func descriptionProse(markdown string) snapshotv1.Prose {
	source := []byte(markdown)
	document := goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser().Parse(text.NewReader(source))
	type heading struct {
		start, end int
		key        string
	}
	var headings []heading
	for node := document.FirstChild(); node != nil; node = node.NextSibling() {
		held, ok := node.(*ast.Heading)
		if !ok || held.Level > 2 || held.Lines().Len() == 0 {
			continue
		}
		first := held.Lines().At(0)
		last := held.Lines().At(held.Lines().Len() - 1)
		start := bytes.LastIndexByte(source[:first.Start], '\n') + 1
		end := lineEnd(source, last.Start)
		if source[start] != '#' && !strings.HasPrefix(strings.TrimSpace(string(source[start:first.Start])), "#") {
			// Setext headings include the underline after their text.
			end = lineEnd(source, end)
		}
		key := ""
		if held.Level == 2 {
			key = proseSection(headingTitle(held, source))
		}
		headings = append(headings, heading{start: start, end: end, key: key})
	}

	var lead strings.Builder
	sections := make(map[string]string)
	cursor := 0
	for index, held := range headings {
		if held.key == "" {
			continue
		}
		end := len(source)
		if index+1 < len(headings) {
			end = headings[index+1].start
		}
		lead.Write(source[cursor:held.start])
		cursor = end
		body := strings.TrimSpace(string(source[held.end:end]))
		sections[held.key] = strings.TrimSpace(sections[held.key] + "\n\n" + body)
	}
	lead.Write(source[cursor:])
	return snapshotv1.Prose{
		Lead: strings.TrimSpace(lead.String()), Auth: sections["auth"],
		Errors: sections["errors"], Limits: sections["limits"],
	}
}

func headingTitle(heading *ast.Heading, source []byte) string {
	var title strings.Builder
	_ = ast.Walk(heading, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node := node.(type) {
		case *ast.Text:
			title.Write(node.Value(source))
			if node.SoftLineBreak() || node.HardLineBreak() {
				title.WriteByte(' ')
			}
		case *ast.String:
			title.Write(node.Value)
		}
		return ast.WalkContinue, nil
	})
	return title.String()
}

func proseSection(title string) string {
	switch strings.ToLower(strings.Join(strings.Fields(title), " ")) {
	case "authorization", "authentication", "auth", "авторизация":
		return "auth"
	case "errors", "ошибки":
		return "errors"
	case "limits", "rate limits", "limitations", "лимиты", "ограничения":
		return "limits"
	default:
		return ""
	}
}

func lineEnd(source []byte, start int) int {
	if offset := bytes.IndexByte(source[start:], '\n'); offset >= 0 {
		return start + offset + 1
	}
	return len(source)
}
