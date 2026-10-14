package openapi

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"schemo/internal/domain"
)

// mediaPreference is the order a body is picked in when an operation offers several: a JSON body is
// the one this app is for, and a form is the one it can still compose by hand.
var mediaPreference = []string{
	domain.MediaJSON,
	"application/vnd.api+json",
	domain.MediaForm,
	domain.MediaMultipart,
	domain.MediaXML,
	domain.MediaText,
}

// applyBody reads the request body of an operation: a 3.x `requestBody` with its media types, or
// the 2.0 parameter that stands in for one. The fields a 2.0 form writes as parameters are already
// rows by the time this is called.
func (r *reader) applyBody(op operation, node *domain.CollectionNode) {
	if op.RequestBody != nil && len(op.RequestBody.Content) > 0 {
		mime, media := pickMedia(op.RequestBody.Content)
		value := exampleOf(media.Example, media.Examples, media.Schema, nil)
		if value == "" {
			value = skeleton(media.Schema)
		}
		applyMedia(node, mime, value, media.Schema)
		return
	}
	for _, param := range op.Parameters {
		if param.In != "body" {
			continue
		}
		value := exampleOf(param.Example, param.Examples, param.Schema, nil)
		if value == "" {
			value = skeleton(param.Schema)
		}
		node.BodyKind = domain.BodyJSON
		node.Body = value
		return
	}
	if len(node.Form) > 0 {
		node.BodyKind = domain.BodyForm
	}
}

func applyMedia(node *domain.CollectionNode, mime, value string, schema map[string]any) {
	switch {
	case mime == domain.MediaMultipart:
		node.BodyKind = domain.BodyForm
		node.Form = formRows(schema)
	case mime == domain.MediaForm:
		node.BodyKind = domain.BodyRaw
		parts := []string{}
		for _, row := range formRows(schema) {
			parts = append(parts, row.Name+"="+row.Value)
		}
		if len(parts) > 0 {
			node.Body = strings.Join(parts, "&")
			return
		}
		node.Body = value
	case strings.Contains(mime, "json"):
		node.BodyKind = domain.BodyJSON
		node.Body = value
	case strings.Contains(mime, "xml"):
		node.BodyKind = domain.BodyXML
		node.Body = value
	default:
		node.BodyKind = domain.BodyRaw
		node.Body = value
	}
}

// pickMedia is the body a request would carry when the document offers several: the first one this
// app is for, and otherwise the first the document writes.
func pickMedia(content map[string]mediaType) (string, mediaType) {
	for _, wanted := range mediaPreference {
		if media, ok := content[wanted]; ok {
			return wanted, media
		}
	}
	for _, name := range slices.Sorted(maps.Keys(content)) {
		return name, content[name]
	}
	return "", mediaType{}
}

// formRows reads a form schema's properties as the fields a form would have.
func formRows(schema map[string]any) []domain.FormRow {
	properties, _ := schema["properties"].(map[string]any)
	out := []domain.FormRow{}
	for _, name := range slices.Sorted(maps.Keys(properties)) {
		field, _ := properties[name].(map[string]any)
		row := domain.FormRow{Name: name, Enabled: true}
		if format, _ := field["format"].(string); format == "binary" {
			row.File = true
		} else {
			row.Value = schemaValue(field)
		}
		out = append(out, row)
	}
	return out
}

// skeleton is a body built from a schema: the shape the document describes, with empty values. It
// is what a person edits before sending, and it says which fields the document expects.
func skeleton(schema map[string]any) string {
	if schema == nil {
		return ""
	}
	value := schemaSample(schema, 0)
	if value == nil {
		return ""
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return ""
	}
	return string(encoded)
}

func schemaSample(schema map[string]any, depth int) any {
	if depth > 6 {
		return nil
	}
	if example, ok := schema["example"]; ok {
		return example
	}
	if settled, ok := schema["default"]; ok {
		return settled
	}
	if all, ok := schema["allOf"].([]any); ok {
		merged := map[string]any{}
		for _, one := range all {
			part, _ := one.(map[string]any)
			if sample, ok := schemaSample(part, depth+1).(map[string]any); ok {
				for key, value := range sample {
					merged[key] = value
				}
			}
		}
		if len(merged) > 0 {
			return merged
		}
	}

	switch kind, _ := schema["type"].(string); kind {
	case "object", "":
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			return nil
		}
		out := map[string]any{}
		for _, name := range slices.Sorted(maps.Keys(properties)) {
			field, _ := properties[name].(map[string]any)
			out[name] = schemaSample(field, depth+1)
		}
		return out
	case "array":
		if items, ok := schema["items"].(map[string]any); ok {
			return []any{schemaSample(items, depth+1)}
		}
		return []any{}
	case "string":
		// The placeholder a person replaces, written the way Hoppscotch writes it: an empty string
		// would make an imported body look like nothing came through.
		return "string"
	case "integer", "number":
		return 0
	case "boolean":
		return true
	default:
		return nil
	}
}

// schemaValue is a scalar from a schema as the text a row carries: a default, the first enum, or
// nothing at all.
func schemaValue(schema map[string]any) string {
	if schema == nil {
		return ""
	}
	for _, key := range []string{"example", "default"} {
		if value, ok := schema[key]; ok && value != nil {
			return domain.ValueText(value)
		}
	}
	if values, ok := schema["enum"].([]any); ok && len(values) > 0 {
		return domain.ValueText(values[0])
	}
	return ""
}
