package command

// httpie, whose body goes behind `--raw=` and whose headers are `Name:value` items after the URL. A
// form with a file becomes items under `--form`; a whole body that is a file is said, not dropped.

import (
	"strings"
)

func renderHTTPie(in commandInput) (string, []Omission) {
	parts := []string{"http"}
	fields := in.formFields()
	switch {
	case len(fields) > 0:
		parts = append(parts, "--form")
	case in.text != "":
		parts = append(parts, "--raw="+shellQuote(in.text))
	}
	parts = append(parts, in.method, shellQuote(in.url))

	for _, row := range fields {
		name := strings.TrimSpace(row.Name)
		if row.File && row.Src != "" {
			parts = append(parts, shellQuote(name+"@"+row.Src))
			continue
		}
		parts = append(parts, shellQuote(name+"="+row.Value))
	}
	for _, h := range in.headers {
		parts = append(parts, shellQuote(h.Name+":"+h.Value))
	}

	var omitted []Omission
	if in.file != "" {
		omitted = in.uploadsOmitted()
	}
	return strings.Join(parts, " "), omitted
}
