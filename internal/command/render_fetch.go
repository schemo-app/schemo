package command

// fetch, written as devtools hands it over: the URL quoted, then an init object. The method is
// always written, even a GET. An init names no file, so a body that is one is left out and named.

import (
	"encoding/json"
)

func renderFetch(in commandInput) (string, []Omission) {
	var head jsonObject
	for _, h := range in.headers {
		head.set(h.Name, json.RawMessage(jsonString(h.Value)))
	}

	init := jsonObject{}
	init.set("method", json.RawMessage(jsonString(in.method)))
	if len(head) > 0 {
		init.set("headers", json.RawMessage(head.text()))
	}

	var omitted []Omission
	switch {
	case in.isUpload():
		omitted = in.uploadsOmitted()
	case in.text != "":
		init.set("body", json.RawMessage(jsonString(in.text)))
	}
	return "fetch(" + shellQuote(in.url) + ", " + init.text() + ")", omitted
}
