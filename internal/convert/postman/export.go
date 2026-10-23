package postman

import (
	"encoding/json"
	"fmt"
	"strings"

	"schemo/internal/domain"
)

// Export writes a collection as a file: the whole collection, or the one request the context menu
// exports. A `{{name}}` stays text and the values a level answers for travel beside it, so the
// collection means the same on the other side.
//
// Nothing needs to be held back as secret: a collection cannot hold one (SaveVariables refuses it),
// so every value written is one the user meant to hand over.
func Export(collection domain.Collection) ([]byte, error) {
	doc := collectionFile{
		Info: info{
			Name: collection.Name, Schema: Schema, Description: writtenText(collection.Description),
		},
		Variable: exportedVariables(collection.Variables),
		Event:    exportedScripts(collection.Scripts),
		Item:     exportLevel(collection),
	}
	if collection.Auth != nil {
		doc.Auth = exportedAuth(*collection.Auth)
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("collection %q could not be written: %w", collection.Name, err)
	}
	return append(data, '\n'), nil
}

// writtenText is a sentence about a level or a request, or nothing at all: a file that writes an
// empty description says the thing has one and it is empty, which is not what an absent one means.
func writtenText(value string) *text {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &text{Value: value}
}

// exportedScripts writes the code a level or request runs as the events Postman runs it from. A
// script switched off is written switched off rather than dropped.
func exportedScripts(scripts *domain.Scripts) []event {
	if scripts == nil {
		return nil
	}
	out := []event{}
	if strings.TrimSpace(scripts.Pre) != "" {
		out = append(out, event{
			Listen:   "prerequest",
			Disabled: scripts.PreOff,
			Script:   script{Exec: lines(strings.Split(scripts.Pre, "\n")), Type: "text/javascript"},
		})
	}
	if strings.TrimSpace(scripts.Post) != "" {
		out = append(out, event{
			Listen:   "test",
			Disabled: scripts.PostOff,
			Script:   script{Exec: lines(strings.Split(scripts.Post, "\n")), Type: "text/javascript"},
		})
	}
	return out
}

// exportLevel writes a level the way the file keeps it: the requests and the collections inside
// them in one list, in the order the tree draws them.
func exportLevel(collection domain.Collection) []item {
	out := make([]item, 0, len(collection.Items)+len(collection.Children))
	for _, entry := range collection.Level() {
		if entry.Collection != nil {
			group := item{
				Name:        entry.Collection.Name,
				Variable:    exportedVariables(entry.Collection.Variables),
				Event:       exportedScripts(entry.Collection.Scripts),
				Description: writtenText(entry.Collection.Description),
			}
			if entry.Collection.Auth != nil {
				group.Auth = exportedAuth(*entry.Collection.Auth)
			}
			group.Item = exportLevel(*entry.Collection)
			out = append(out, group)
			continue
		}
		out = append(out, item{
			Name:        entry.Node.Name,
			Event:       exportedScripts(entry.Node.Scripts),
			Description: writtenText(entry.Node.Description),
			Request:     exportedRequest(*entry.Node),
			Response:    exportedExamples(entry.Node.Responses),
		})
	}
	return out
}

// exportedExamples writes the saved answers as the file keeps them: name, code, headers and body.
// The request each one answered is the request it sits beside and is not written twice.
func exportedExamples(examples []domain.ResponseExample) []example {
	out := make([]example, 0, len(examples))
	for _, one := range examples {
		saved := example{Name: one.Name, Code: one.Status, Body: one.Body}
		for _, header := range one.Headers {
			if strings.TrimSpace(header.Name) == "" {
				continue
			}
			saved.Header = append(saved.Header, field{Key: header.Name, Value: header.Value})
		}
		out = append(out, saved)
	}
	return out
}

// exportedVariables writes what a level answers for `{{tokens}}`. A row with no name is not a
// variable — it is a row somebody left empty, and a name is the only thing that makes one.
func exportedVariables(variables []domain.Variable) []variable {
	out := make([]variable, 0, len(variables))
	for _, v := range variables {
		if v.Name == "" {
			continue
		}
		out = append(out, variable{
			Key:      v.Name,
			Value:    v.Value,
			Type:     "string",
			Disabled: !v.Enabled,
		})
	}
	return out
}

func exportedRequest(node domain.CollectionNode) *request {
	out := &request{
		Method: node.Method,
		URL: url{
			Raw: node.URL,
		},
	}
	for _, header := range node.Headers {
		if strings.TrimSpace(header.Name) == "" {
			continue
		}
		out.Header = append(out.Header, field{
			Key: header.Name, Value: header.Value, Disabled: !header.Enabled,
			Description: writtenText(header.Description),
		})
	}
	// The parameters are written beside the address and not only inside it: a switched-off row is
	// not in the address, and this is the only place it can travel.
	for _, param := range node.Params {
		if strings.TrimSpace(param.Name) == "" {
			continue
		}
		out.URL.Query = append(out.URL.Query, field{
			Key: param.Name, Value: param.Value, Disabled: !param.Enabled,
			Description: writtenText(param.Description),
		})
	}
	for _, row := range node.PathParams {
		if strings.TrimSpace(row.Name) == "" {
			continue
		}
		out.URL.Variable = append(out.URL.Variable, field{
			Key: row.Name, Value: row.Value, Description: writtenText(row.Description),
		})
	}
	out.Body = exportedBody(node)
	if node.Auth != nil {
		out.Auth = exportedAuth(*node.Auth)
	}
	return out
}

// json, xml and raw all leave as `raw`: Postman cannot record which of the three a body was, and an
// `options.raw.language` this reader ignores would be a promise the next import breaks. Forms and
// files survive the trip.
func exportedBody(node domain.CollectionNode) *body {
	switch domain.KindOf(node.BodyKind) {
	case domain.BodyForm:
		out := &body{Mode: "formdata"}
		for _, row := range node.Form {
			if strings.TrimSpace(row.Name) == "" {
				continue
			}
			out.FormData = append(out.FormData, field{
				Key: row.Name, Value: row.Value, Src: row.Src, Disabled: !row.Enabled,
			})
		}
		return out
	case domain.BodyBinary:
		if node.BodyFile == "" {
			return nil
		}
		return &body{Mode: "file", File: &fileField{Src: node.BodyFile}}
	default:
		if node.Body == "" {
			return nil
		}
		return &body{Mode: "raw", Raw: node.Body}
	}
}

// Every field the scheme declares is written, empty or not, since a missing field reads as one that
// does not exist. The order is the scheme's own, which is Postman's drawing order.
func exportedAuth(from domain.Auth) *auth {
	// A request that sends nothing says so in the file's own word for it; left out, it would come
	// back as one that inherits.
	if from.Type == domain.AuthNone {
		return &auth{Type: "noauth"}
	}
	written, ok := schemes[from.Type]
	if !ok {
		return nil
	}
	out := &auth{Type: written.Name, Extra: map[string][]field{}}
	fields := []field{}
	for _, key := range domain.FieldKeys(from.Type) {
		name, ok := written.Fields[key]
		if !ok {
			continue
		}
		fields = append(fields, field{Key: name, Value: from.Answer(key)})
	}
	out.Extra[written.Name] = fields
	return out
}
