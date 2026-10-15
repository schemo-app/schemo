package update

import "strings"

// ParseNotes extracts bullet points (starting with '-' or '*') from a release body text.
// Non-bullet lines, headers, and empty items are ignored.
func ParseNotes(body string) []string {
	var notes []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		item, ok := strings.CutPrefix(line, "- ")
		if !ok {
			item, ok = strings.CutPrefix(line, "* ")
		}
		if !ok {
			continue
		}
		if item = strings.TrimSpace(item); item != "" {
			notes = append(notes, item)
		}
	}
	return notes
}
