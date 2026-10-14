package command

// wget, whose flags carry their value with an `=`: `--method=POST`, `--header=…`, `--body-data=…`.
// `--body-data` is text and nothing else, so a file body goes out without one, and says so.

import (
	"strings"
)

func renderWget(in commandInput) (string, []Omission) {
	parts := []string{"wget"}
	if in.method != "GET" {
		parts = append(parts, "--method="+in.method)
	}
	for _, h := range in.headers {
		parts = append(parts, "--header="+shellQuote(h.Name+": "+h.Value))
	}

	var omitted []Omission
	switch {
	case in.isUpload():
		omitted = in.uploadsOmitted()
	case in.text != "":
		parts = append(parts, "--body-data="+shellQuote(in.text))
	}
	parts = append(parts, shellQuote(in.url))
	return strings.Join(parts, " "), omitted
}
