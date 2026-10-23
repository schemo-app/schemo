package command

// Invoke-RestMethod, whose headers are one `@{ … }` hashtable and whose quotes double rather than
// close and reopen. `-InFile` carries a file body; a form that has one is said, not half written.

import (
	"strings"
)

func renderPowerShell(in commandInput) (string, []Omission) {
	parts := []string{"Invoke-RestMethod -Method " + in.method + " -Uri " + psQuote(in.url)}
	if len(in.headers) > 0 {
		pairs := make([]string, 0, len(in.headers))
		for _, h := range in.headers {
			pairs = append(pairs, psQuote(h.Name)+" = "+psQuote(h.Value))
		}
		parts = append(parts, "-Headers @{ "+strings.Join(pairs, "; ")+" }")
	}

	var omitted []Omission
	switch {
	case in.file != "":
		parts = append(parts, "-InFile "+psQuote(in.file))
	case len(in.fields) > 0:
		omitted = in.uploadsOmitted()
	case in.text != "":
		parts = append(parts, "-Body "+psQuote(in.text))
	}
	return strings.Join(parts, " "), omitted
}
