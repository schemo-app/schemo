// Package postman reads and writes the Postman v2.1 collection format: bytes in, a
// domain.Collection out, and back again. It is a pure package — no window, no database — so
// both directions can be tested against a file.
package postman

import (
	"encoding/json"
	"strings"
)

// Schema is what a file written here says it is. Postman reads it, and so does anyone else looking
// at the JSON.
const Schema = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"

type collectionFile struct {
	Info        info       `json:"info"`
	Auth        *auth      `json:"auth,omitempty"`
	Variable    []variable `json:"variable,omitempty"`
	Event       []event    `json:"event,omitempty"`
	Description *text      `json:"description,omitempty"`
	Item        []item     `json:"item"`
}

type info struct {
	Name        string `json:"name"`
	Schema      string `json:"schema,omitempty"`
	Description *text  `json:"description,omitempty"`
}

// text is what a file says about a collection, a folder or a request: a string in files written by
// hand, and an object with a `content` in the ones Postman writes itself.
type text struct {
	Value string
}

func (t *text) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		return json.Unmarshal(data, &t.Value)
	}
	var object struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	t.Value = object.Content
	return nil
}

func (t text) MarshalJSON() ([]byte, error) { return json.Marshal(t.Value) }

// item is a row of a collection file: a request is the thing that gets sent, a folder holds more.
// A folder is this app's "collection inside one" and keeps its own auth: a group that lost it on
// the way in would behave differently here.
type item struct {
	Name        string     `json:"name"`
	Auth        *auth      `json:"auth,omitempty"`
	Variable    []variable `json:"variable,omitempty"`
	Event       []event    `json:"event,omitempty"`
	Description *text      `json:"description,omitempty"`
	Item        []item     `json:"item,omitempty"`
	Request     *request   `json:"request,omitempty"`
	Response    []example  `json:"response,omitempty"`
}

// example is one saved answer Postman writes beside a request: what it is called, the code it
// carried, its headers and its body as they were recorded. The original request it also carries is
// the request it sits beside, said twice, and is not read.
type example struct {
	Name   string  `json:"name,omitempty"`
	Code   int     `json:"code,omitempty"`
	Header []field `json:"header,omitempty"`
	Body   string  `json:"body,omitempty"`
}

// event is code Postman runs around the request: "prerequest" before it is sent and "test" after
// the answer. What the file marks disabled is code somebody wrote and switched off, so it arrives
// switched off rather than dropped.
type event struct {
	Listen   string `json:"listen"`
	Disabled bool   `json:"disabled,omitempty"`
	Script   script `json:"script"`
}

type script struct {
	Exec lines  `json:"exec,omitempty"`
	Type string `json:"type,omitempty"`
}

// lines is script code as a file writes it: a list of lines in Postman's own files and one string
// with newlines in files assembled by hand.
type lines []string

func (l *lines) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		*l = strings.Split(strings.TrimRight(raw, "\n"), "\n")
		return nil
	}
	type list []string
	return json.Unmarshal(data, (*list)(l))
}

// variable is one `{{name}}` a level answers for, as a file writes it. The value is read as any
// JSON and not as a string: the format allows a number or a flag, and a value dropped for being
// written without quotes would be a variable that quietly changed meaning on the way in.
//
// What a file calls its type is not kept. Postman's own collection variables are strings, flags,
// numbers or "any" and never a secret — a secret belongs to an environment there — and what this
// app substitutes is text in every case, so the type says nothing name and value have not said.
type variable struct {
	Key      string `json:"key"`
	Value    any    `json:"value,omitempty"`
	Type     string `json:"type,omitempty"`
	Secret   bool   `json:"secret,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
	// enabled is how an environment file spells the same thing: it writes `enabled: false` for a
	// value somebody switched off, where a collection writes `disabled: true`.
	Enabled *bool `json:"enabled,omitempty"`
}

// secret is whether the file marks the value as one to keep out of sight: Postman's own files say
// `type: "secret"` and the newer ones also write a flag beside it.
func (v variable) secret() bool { return v.Secret || v.Type == "secret" }

// switchedOn is whether the tool this file came from would substitute the value.
func (v variable) switchedOn() bool {
	if v.Enabled != nil {
		return *v.Enabled
	}
	return !v.Disabled
}

type request struct {
	Method      string  `json:"method"`
	Header      []field `json:"header,omitempty"`
	Body        *body   `json:"body,omitempty"`
	URL         url     `json:"url"`
	Auth        *auth   `json:"auth,omitempty"`
	Description *text   `json:"description,omitempty"`
}

// field is a key and a value, as Postman writes headers, query parameters and form rows. A row the
// file marks disabled is kept disabled: it is a thing about the request, not a comment.
type field struct {
	Key         string `json:"key"`
	Value       string `json:"value,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
	Src         string `json:"src,omitempty"`
	Description *text  `json:"description,omitempty"`
}

// url is a string in some files and an object in others, which is why it reads itself: Postman
// writes the object, and a hand-written file usually writes the string.
type url struct {
	Raw   string  `json:"raw"`
	Query []field `json:"query,omitempty"`
	// Variable holds the values of the `:name` segments of the address, which Postman writes beside
	// it and not in it.
	Variable []field `json:"variable,omitempty"`
}

func (u *url) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		u.Raw = raw
		return nil
	}
	// A named type, so this method is not called again for the same bytes.
	type object url
	return json.Unmarshal(data, (*object)(u))
}

type body struct {
	Mode       string     `json:"mode"`
	Raw        string     `json:"raw,omitempty"`
	URLEncoded []field    `json:"urlencoded,omitempty"`
	FormData   []field    `json:"formdata,omitempty"`
	File       *fileField `json:"file,omitempty"`
}

// A path and not the bytes, which is what Postman keeps and what this app keeps: a collection file
// stays a description of the requests in it rather than a copy of everything they send.
type fileField struct {
	Src string `json:"src,omitempty"`
}

// auth is an authorization as Postman writes it: the scheme's name, and its fields under a key of
// that same name — `{"type":"bearer","bearer":[{"key":"token","value":"…"}]}`. The fields are a map
// and not a struct because they belong to the scheme, and two schemes differ from the rest.
type auth struct {
	Type  string             `json:"type"`
	Extra map[string][]field `json:"-"`
}

func (a auth) MarshalJSON() ([]byte, error) {
	out := make(map[string]any, len(a.Extra)+1)
	out["type"] = a.Type
	for name, fields := range a.Extra {
		out[name] = fields
	}
	return json.Marshal(out)
}

func (a *auth) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	a.Extra = map[string][]field{}
	for name, value := range raw {
		if name == "type" {
			if err := json.Unmarshal(value, &a.Type); err != nil {
				return err
			}
			continue
		}
		// A scheme this app does not know may carry something that is not a list of fields — Postman
		// has half a dozen of those. Refusing the whole file over one is worse than ignoring it.
		var fields []field
		if err := json.Unmarshal(value, &fields); err != nil {
			continue
		}
		a.Extra[name] = fields
	}
	return nil
}
