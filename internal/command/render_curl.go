package command

// curl, written the way devtools writes it: `-H` headers, `--data-raw` body, no `-X` for a GET.
// The one notation that can carry a file: `--data-binary @file` and `-F name=@file`.

import (
	"strings"
)

func renderCurl(in commandInput, lined bool) (string, []Omission) {
	// The first line keeps the tool and the method; every option after it is a line of its own when
	// the command is written for a file, and the whole thing is one line when it is written for a
	// terminal.
	head := "curl"
	if in.method != "GET" {
		head += " -X " + in.method
	}
	lines := []string{head}
	for _, h := range in.headers {
		lines = append(lines, "-H "+shellQuote(h.Name+": "+h.Value))
	}
	lines = append(lines, curlPairs(curlBody(in))...)
	lines = append(lines, shellQuote(in.url))
	if lined {
		return strings.Join(lines, " \\\n  "), nil
	}
	return strings.Join(lines, " "), nil
}

// curlPairs is the body with each option on one line: the body comes as flags and their values, and
// a continuation between the two would split an option from what it carries.
func curlPairs(parts []string) []string {
	out := []string{}
	for i := 0; i < len(parts); i += 2 {
		if i+1 < len(parts) {
			out = append(out, parts[i]+" "+parts[i+1])
			continue
		}
		out = append(out, parts[i])
	}
	return out
}

// curlBody is the body in curl's own terms. The whole of `name=@file` stands in one pair of quotes:
// a path with a space in it is a path like any other, and curl reads the argument whole.
func curlBody(in commandInput) []string {
	switch {
	case in.file != "":
		return []string{"--data-binary", "@" + shellQuote(in.file)}
	case len(in.fields) > 0:
		out := make([]string, 0, 2*len(in.fields))
		for _, row := range in.formFields() {
			name := strings.TrimSpace(row.Name)
			if row.File && row.Src != "" {
				out = append(out, "-F", shellQuote(name+"=@"+row.Src))
				continue
			}
			out = append(out, "-F", shellQuote(name+"="+row.Value))
		}
		return out
	case in.text != "":
		return []string{"--data-raw", shellQuote(in.text)}
	}
	return nil
}
