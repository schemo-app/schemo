package command_test

import (
	"strings"
	"testing"

	"schemo/internal/convert/command"
	"schemo/internal/domain"
)

// A command written for a file has one option per line, continued with `\`: a nine-header request
// on one line is a command nobody reads. The terminal's own copy stays the single line it was.
func TestRenderCurlLinesBreaksEveryOption(t *testing.T) {
	request := command.Request{
		Method: "POST",
		URL:    "https://api.example.com/users",
		Headers: []domain.HeaderPair{
			{Name: "Accept", Value: "application/json"},
			{Name: "X-Tenant", Value: "example"},
		},
		Body: `{"name":"Ada"}`, BodyKind: domain.BodyJSON,
	}

	oneLine, _ := command.Render(command.FormatCurl, request)
	if strings.Contains(oneLine, "\n") {
		t.Errorf("command = %q, want one line for a terminal", oneLine)
	}

	lined, _ := command.Render(command.FormatCurlLines, request)
	lines := strings.Split(lined, "\n")
	if len(lines) != 5 {
		t.Fatalf("command = %q, want the head, two headers, the body and the address", lined)
	}
	if lines[0] != "curl -X POST \\" {
		t.Errorf("first line = %q, want the tool and the method", lines[0])
	}
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, "  ") {
			t.Errorf("line = %q, want a continuation indented under the command", line)
		}
	}
	for _, line := range lines[:len(lines)-1] {
		if !strings.HasSuffix(line, " \\") {
			t.Errorf("line = %q, want a continuation", line)
		}
	}
	if !strings.HasSuffix(lines[len(lines)-1], "'https://api.example.com/users'") {
		t.Errorf("last line = %q, want the address and nothing after it", lines[len(lines)-1])
	}
}
