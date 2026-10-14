package command

// The JSON a rendered command carries: the init of a `fetch(url, {…})` and the strings inside it.
// An object that remembers its key order, because Go iterates a map differently every time.

import (
	"encoding/json"
	"strings"
)

type jsonObject []jsonField

type jsonField struct {
	key   string
	value json.RawMessage
}

// Mirrors a plain JS object: a key already set keeps its position and takes the new value.
func (o *jsonObject) set(key string, value json.RawMessage) {
	for i := range *o {
		if (*o)[i].key == key {
			(*o)[i].value = value
			return
		}
	}
	*o = append(*o, jsonField{key: key, value: value})
}

func (o jsonObject) text() string {
	var b strings.Builder
	b.WriteByte('{')
	for i, f := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(jsonString(f.key))
		b.WriteByte(':')
		b.Write(f.value)
	}
	b.WriteByte('}')
	return b.String()
}

// jsonString writes a JSON string literal with Go's encoder and HTML escaping off: `<` is an
// ordinary character in a body somebody wrote.
func jsonString(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}
