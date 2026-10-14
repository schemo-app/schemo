package domain

import "encoding/json"

// Document is what a file of another tool turned out to hold, in this app's terms: the collections
// to write into the tree and the environments to write into the environment list. A format that
// carries only one of the two answers with an empty other half — a Postman collection file holds no
// environment, a Postman environment file holds no collection.
type Document struct {
	Collections  []Collection       `json:"collections,omitempty"`
	Environments []EnvironmentDraft `json:"environments,omitempty"`
	Skipped      []Skip             `json:"skipped,omitempty"`
}

// EnvironmentDraft is an environment a file describes: the name it gives it, the colour it asks for
// and the variables it holds. Ids, positions and which environment is active are the tree's to
// decide where the draft is written.
type EnvironmentDraft struct {
	Name      string     `json:"name"`
	Color     string     `json:"color,omitempty"`
	Variables []Variable `json:"variables,omitempty"`
}

// Skip is one thing an import deliberately left out, counted rather than carried: a code the window
// has a sentence for, and how many of them there were. A count, never the things themselves.
type Skip struct {
	Code  Code `json:"code"`
	Count int  `json:"count"`
}

// LeftOut records that something of a kind was not carried over. Readers call it while they walk a
// file, so what a person is told at the end is what the file actually held, not what the format
// could have held.
func (d *Document) LeftOut(code Code, count int) {
	if count <= 0 {
		return
	}
	for index := range d.Skipped {
		if d.Skipped[index].Code == code {
			d.Skipped[index].Count += count
			return
		}
	}
	d.Skipped = append(d.Skipped, Skip{Code: code, Count: count})
}

// ValueText is a value out of a file as the text a request substitutes: a string as it is and
// anything else as its JSON, so a number keeps its digits and `2` and `"2"` answer `{{page}}` the
// same way.
func ValueText(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}
