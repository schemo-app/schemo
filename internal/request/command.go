package request

// Pasted text read as the requests it holds, for the import's cURL source, and the query string as
// rows and back.

import (
	"regexp"
	"strings"

	"schemo/internal/convert/command"
	"schemo/internal/domain"
)

// commandStart finds where one command begins in a paste: the tool it is written with, at the start
// of a line, with whatever a terminal puts in front of it.
var commandStart = regexp.MustCompile(`(?m)^[ \t]*(?:\$|>|sudo[ \t]+)?curl(?:\.exe)?\b`)

// ReadCommands reads pasted text as the requests it holds, one per curl command, for the import's
// cURL source. A `# name` line directly above a command names the request it precedes, which is how
// this app's own copy writes commands. Text with no command in it is refused.
func ReadCommands(text string) ([]domain.CollectionNode, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	starts := commandStart.FindAllStringIndex(text, -1)
	if len(starts) == 0 {
		return nil, domain.Refuse(domain.CodeNotCommand, domain.ErrNotAllowed, nil)
	}

	out := []domain.CollectionNode{}
	for index, at := range starts {
		end := len(text)
		if index+1 < len(starts) {
			end = starts[index+1][0]
		}
		reading := command.Parse(dropTrailing(text[at[0]:end]))
		if reading.Kind != command.KindOK {
			continue
		}
		out = append(out, nodeOfCommand(reading.Request, commentAbove(text, at[0]), len(out)))
	}
	if len(out) == 0 {
		return nil, domain.Refuse(domain.CodeNotCommand, domain.ErrNotAllowed, nil)
	}
	return out, nil
}

// dropTrailing removes the blank and comment lines a block picked up from before the next command:
// a `# name` line belongs to the command under it, not to the one above.
func dropTrailing(block string) string {
	lines := strings.Split(block, "\n")
	for len(lines) > 0 {
		last := strings.TrimSpace(lines[len(lines)-1])
		if last != "" && !strings.HasPrefix(last, "#") {
			break
		}
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// commentAbove is the name a `#` line directly above a command gives it. Any other comment is a
// comment, and a blank line ends the naming: it was about something else.
func commentAbove(text string, at int) string {
	before := strings.TrimRight(text[:at], " \t\n")
	if cut := strings.LastIndexByte(before, '\n'); cut >= 0 {
		before = before[cut+1:]
	}
	name, ok := strings.CutPrefix(strings.TrimSpace(before), "#")
	if !ok {
		return ""
	}
	return strings.TrimSpace(name)
}

// nodeOfCommand is one parsed command as a request. The address keeps its query (the one place a
// query lives) and the rows beside it are what that query reads as, all enabled: what a command
// carries was sent.
func nodeOfCommand(from command.Request, name string, position int) domain.CollectionNode {
	node := domain.CollectionNode{
		Name:     name,
		Position: int64(position),
		Method:   from.Method,
		URL:      from.URL,
		Params:   paramsFromURL(from.URL),
		Body:     from.Body,
		BodyKind: kindOfBody(from),
		Form:     from.Form,
		BodyFile: from.BodyFile,
		Auth:     from.Auth,
	}
	for _, header := range from.Headers {
		node.Headers = append(node.Headers, domain.Row{
			Name: header.Name, Value: header.Value, Enabled: true,
		})
	}
	if node.Name == "" {
		node.Name = from.URL
	}
	return node
}

// kindOfBody is what the body's own declaration names: a command sends a form when it carried form
// fields and says the rest with a Content-Type header, and the editor should open on that kind.
func kindOfBody(from command.Request) domain.BodyKind {
	if from.BodyKind != "" {
		return from.BodyKind
	}
	if len(from.Form) > 0 {
		return domain.BodyForm
	}
	for _, header := range from.Headers {
		if !strings.EqualFold(header.Name, "Content-Type") {
			continue
		}
		media := domain.MediaTypeOf(header.Value)
		switch {
		case domain.IsJSONMedia(media):
			return domain.BodyJSON
		case domain.IsXMLMedia(media):
			return domain.BodyXML
		}
		break
	}
	if from.Body != "" {
		return domain.BodyRaw
	}
	return ""
}

// paramsFromURL reads the query string into rows. A query string is the one place a value is
// percent-encoded, so this is where the decoding happens — the rows hold what the user typed. The
// fragment is not a parameter, and a URL that has one survives a round trip through the rows.
func paramsFromURL(raw string) []domain.Row {
	_, query := domain.SplitQuery(raw)
	if query == "" {
		return nil
	}

	out := []domain.Row{}
	for _, pair := range domain.QueryPairs(query) {
		decoded := pair.Decoded()
		out = append(out, domain.Row{Name: decoded.Name, Value: decoded.Value, Enabled: true})
	}
	return out
}

// Written by hand rather than with url.QueryEscape: both it and the browser escape `,` and
// brackets, while this app's URLs are `include=author,comments` and `filter[id][in]=1,2`. All three
// are legal in a query and stay as typed; the rest follows the standard rule, workspace as `+`.
func encodeQuery(text string) string {
	var out strings.Builder
	out.Grow(len(text))

	for i := 0; i < len(text); i++ {
		switch c := text[i]; {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9',
			c == '*', c == '-', c == '.', c == '_', c == '~', c == ',', c == '[', c == ']':
			out.WriteByte(c)
		case c == ' ':
			out.WriteByte('+')
		default:
			out.WriteString("%")
			out.WriteByte("0123456789ABCDEF"[c>>4])
			out.WriteByte("0123456789ABCDEF"[c&0x0f])
		}
	}

	// The braces are put back last: `{{name}}` is what the user typed and what the resolver looks
	// for, and `%7B%7Bname%7D%7D` is a token nobody would recognise.
	escaped := out.String()
	escaped = strings.ReplaceAll(escaped, "%7B%7B", "{{")
	return strings.ReplaceAll(escaped, "%7D%7D", "}}")
}
