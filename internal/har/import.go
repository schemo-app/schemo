package har

import (
	"encoding/json"
	"strings"

	"schemo/internal/domain"
)

// Import reads a HAR log as one collection: an entry becomes a request. What the log recorded of
// the answers belongs to a history, not to a collection — a request this app sends composes its own
// answer — so responses and timings are left out and counted, and cookies are counted too: a jar is
// not something a file can fill.
func Import(data []byte) (domain.Document, error) {
	var file logFile
	if err := json.Unmarshal(data, &file); err != nil {
		return domain.Document{}, domain.Refuse(domain.CodeNotJSON, domain.ErrNotAllowed, nil)
	}
	if file.Log.Version == "" && len(file.Log.Entries) == 0 {
		return domain.Document{}, domain.Refuse(domain.CodeNotHAR, domain.ErrNotAllowed, nil)
	}

	var doc domain.Document
	// One flat collection, named the constant Hoppscotch names it: a log has no name of its own,
	// and the requests inside carry their whole addresses — the host is read off each one rather
	// than repeated as a level.
	collection := domain.Collection{Name: "Imported from HAR"}
	for _, one := range file.Log.Entries {
		collection.Items = append(collection.Items, requestNode(one.Request, &doc))
	}

	doc.Collections = append(doc.Collections, collection)
	return doc, nil
}

// requestNode is one entry as a request. The query is taken from the address it was written in and
// the list beside it is read into the parameter rows: what the log recorded as sent was sent, so
// every row of it is a row that was on.
func requestNode(from request, doc *domain.Document) domain.CollectionNode {
	node := domain.CollectionNode{
		Name:   from.URL,
		Method: strings.ToUpper(strings.TrimSpace(from.Method)),
		URL:    from.URL,
	}

	for _, header := range from.Headers {
		// The pseudo-headers are HTTP/2's own: they are not headers a request here can carry, and
		// `:method` and `:path` are said twice otherwise.
		if strings.HasPrefix(header.Name, ":") || strings.TrimSpace(header.Name) == "" {
			continue
		}
		node.Headers = append(node.Headers, domain.Row{
			Name: header.Name, Value: header.Value, Enabled: true,
		})
	}
	for _, query := range from.QueryString {
		if strings.TrimSpace(query.Name) == "" {
			continue
		}
		node.Params = append(node.Params, domain.Row{Name: query.Name, Value: query.Value, Enabled: true})
	}
	applyPostData(&node, from.PostData, doc)

	if len(from.Cookies) > 0 {
		doc.LeftOut(domain.CodeSkippedCookieJar, 1)
	}
	return node
}

// applyPostData reads a body as the log recorded it. A body the format stored as base64 is left out
// rather than decoded into something this app would send back as text.
func applyPostData(node *domain.CollectionNode, from *postData, doc *domain.Document) {
	if from == nil {
		return
	}
	if from.Encoding == "base64" {
		doc.LeftOut(domain.CodeSkippedBase64Body, 1)
		return
	}

	mime := mimeOf(from.MimeType)
	switch {
	case strings.HasPrefix(mime, domain.MediaMultipart):
		node.BodyKind = domain.BodyForm
		for _, param := range from.Params {
			row := domain.FormRow{Name: param.Name, Value: param.Value, Enabled: true}
			if param.FileName != "" {
				// The field survives and the file does not: a path in the log is where the browser
				// read it, not something this app can send.
				row.File = true
				row.Value = ""
				doc.LeftOut(domain.CodeSkippedBinaryFile, 1)
			}
			node.Form = append(node.Form, row)
		}
	case mime == domain.MediaForm:
		node.BodyKind = domain.BodyRaw
		if from.Text != "" {
			node.Body = from.Text
			return
		}
		parts := make([]string, 0, len(from.Params))
		for _, param := range from.Params {
			parts = append(parts, param.Name+"="+param.Value)
		}
		node.Body = strings.Join(parts, "&")
	default:
		node.BodyKind = domain.BodyRaw
		node.Body = from.Text
	}
}

// mimeOf is the media type without its parameters: `application/json; charset=utf-8` is json.
func mimeOf(value string) string {
	if at := strings.Index(value, ";"); at >= 0 {
		value = value[:at]
	}
	return strings.ToLower(strings.TrimSpace(value))
}
