package har

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"schemo/internal/domain"
)

// Import reads a HAR log as one collection, a request per entry.
func Import(data []byte) (domain.Document, error) {
	return Read(bytes.NewReader(data))
}

// Read imports one entry at a time. Response bodies can dominate the file size and are discarded.
func Read(source io.Reader) (domain.Document, error) {
	decoder := json.NewDecoder(source)
	var doc domain.Document
	var version string
	collection := domain.Collection{Name: "Imported from HAR"}
	err := readObject(decoder, func(key string) error {
		if !strings.EqualFold(key, "log") {
			return decoder.Decode(&discardedJSON{})
		}
		return readObject(decoder, func(key string) error {
			switch strings.ToLower(key) {
			case "version":
				return decoder.Decode(&version)
			case "entries":
				collection.Items = nil
				doc.Skipped = nil
				return readEntries(decoder, &collection, &doc)
			default:
				return decoder.Decode(&discardedJSON{})
			}
		})
	})
	if err != nil || decoder.Decode(&discardedJSON{}) != io.EOF {
		return domain.Document{}, domain.Refuse(domain.CodeNotJSON, domain.ErrNotAllowed, nil)
	}
	if version == "" && len(collection.Items) == 0 {
		return domain.Document{}, domain.Refuse(domain.CodeNotHAR, domain.ErrNotAllowed, nil)
	}
	doc.Collections = []domain.Collection{collection}
	return doc, nil
}

func readObject(decoder *json.Decoder, field func(string) error) error {
	return readContainer(decoder, '{', func() error {
		key, err := decoder.Token()
		if err != nil {
			return err
		}
		return field(key.(string))
	})
}

func readEntries(decoder *json.Decoder, collection *domain.Collection, doc *domain.Document) error {
	return readContainer(decoder, '[', func() error {
		var one struct {
			Request request `json:"request"`
		}
		if err := decoder.Decode(&one); err != nil {
			return err
		}
		collection.Items = append(collection.Items, requestNode(one.Request, doc))
		return nil
	})
}

// readContainer walks one object or array token by token, so a large log is never held whole. A
// null stands for an empty container.
func readContainer(decoder *json.Decoder, open json.Delim, each func() error) error {
	start, err := decoder.Token()
	if err != nil || start == nil {
		return err
	}
	if start != open {
		return fmt.Errorf("expected %q", open)
	}
	for decoder.More() {
		if err := each(); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

type discardedJSON struct{}

func (*discardedJSON) UnmarshalJSON([]byte) error { return nil }

// requestNode is one entry as a request. The query comes from the address and the list beside it is
// read into parameter rows: what the log recorded as sent was sent, so every row is on.
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

	mime := domain.MediaTypeOf(from.MimeType)
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
