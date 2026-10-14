package draft

// A saved request seen as something to compose, and pasted text seen as the requests it holds:
// both are the draft feature's own reading of the model, and both are used from outside it — the
// card opens a node as a draft, and the import reads a paste of commands with no draft around it.

import (
	"regexp"
	"strings"

	"schemo/internal/command"
	"schemo/internal/domain"
)

// FromNode is a saved request seen as something to edit. Rows keep their ids, so an editor that is
// open on one of them stays on it. A node with no auth of its own inherits one, and «Inherit» is
// how the draft says that: the card draws the choice, and the tree is asked for the answer when the
// request goes out.
func FromNode(node domain.CollectionNode) domain.Draft {
	auth := domain.Auth{Type: domain.AuthInherit}
	if node.Auth != nil {
		auth = *node.Auth
	}
	return domain.Draft{
		ID:            domain.DraftID(node.ID),
		Method:        node.Method,
		URL:           node.URL,
		Params:        domain.OrEmpty(node.Params),
		Headers:       domain.OrEmpty(node.Headers),
		PathParams:    domain.OrEmpty(node.PathParams),
		Body:          node.Body,
		BodyKind:      domain.KindOf(node.BodyKind),
		Form:          domain.OrEmpty(node.Form),
		BodyFile:      node.BodyFile,
		Auth:          auth,
		EnvironmentID: node.EnvironmentID,
	}
}

// commandStart finds where one command begins in a paste: the tool it is written with, at the start
// of a line, with whatever a terminal puts in front of it.
var commandStart = regexp.MustCompile(`(?m)^[ \t]*(?:\$|>|sudo[ \t]+)?curl(?:\.exe)?\b`)

// ReadCommands reads pasted text as the requests it holds, one per curl command, for the import's
// cURL source. A `# name` line directly above a command names the request it precedes — which is
// how this app's own copy writes commands down. Text with no command in it is refused.
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

// nodeOfCommand is one parsed command as a request. The address keeps its query — the address is
// the one place a query lives — and the rows beside it are what that query reads as, all of them
// enabled: what a command carries was sent.
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
		mime := strings.ToLower(header.Value)
		switch {
		case strings.HasPrefix(mime, "application/json"), strings.Contains(mime, "+json"):
			return domain.BodyJSON
		case strings.Contains(mime, "xml"):
			return domain.BodyXML
		}
		break
	}
	if from.Body != "" {
		return domain.BodyRaw
	}
	return ""
}
