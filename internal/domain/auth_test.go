package domain

import (
	"testing"
)

// The registry is the one place that knows what a scheme asks for, and the window draws whatever it
// says. A row that contradicts itself — a select with no choices, a field with no name — is a hole
// in that drawing rather than a mistake anybody would notice at the point of use.
func TestEverySchemeDescribesItself(t *testing.T) {
	seen := map[AuthType]bool{}
	primary := 0

	for _, scheme := range AuthSchemes() {
		t.Run(string(scheme.Type), func(t *testing.T) {
			if scheme.Type == "" {
				t.Fatal("a scheme with no type cannot be chosen")
			}
			if seen[scheme.Type] {
				t.Fatalf("two schemes answer to %q", scheme.Type)
			}
			seen[scheme.Type] = true
			if scheme.Primary {
				primary++
			}

			keys := map[string]bool{}
			for _, field := range scheme.Fields {
				if field.Key == "" || field.Label == "" {
					t.Errorf("a field with no key or no label cannot be drawn: %+v", field)
				}
				if keys[field.Key] {
					t.Errorf("two fields answer to %q", field.Key)
				}
				keys[field.Key] = true

				if field.Kind == FieldSelect {
					if len(field.Options) == 0 {
						t.Errorf("field %q is a select with nothing to choose", field.Key)
					}
					if field.Default != "" && !hasOption(field.Options, field.Default) {
						t.Errorf("field %q starts on %q, which is not one of its choices", field.Key, field.Default)
					}
				} else if len(field.Options) > 0 {
					t.Errorf("field %q carries choices but is drawn as %q", field.Key, field.Kind)
				}
				if field.Default != "" && field.Kind == FieldSelect && field.Secret {
					t.Errorf("field %q is a secret with a value filled in for it", field.Key)
				}
			}

			// The request pane says beside the chosen scheme what it sends — all a scheme with no
			// fields has to say for itself, and without it the pane's header would be half empty.
			if scheme.Sends == "" {
				t.Error("a scheme that does not say what it sends leaves the line beside it blank")
			}

			if scheme.NeedsParent && scheme.Type != AuthInherit {
				t.Errorf("%q says it needs a level above it, which only 'Inherit' means", scheme.Type)
			}
		})
	}

	// The segmented control is drawn from the same list, and every scheme in the "More" menu is one
	// nobody reached by accident: a registry that named none of them primary would have no control.
	if primary == 0 {
		t.Error("every scheme is behind 'More': there would be no segmented control at all")
	}
}

// A scheme opens on its own defaults — Normalized is what fills them — so a select never starts
// blank: an empty select is a question the user has to answer before the scheme means anything.
func TestDefaultsAreFilledIn(t *testing.T) {
	auth := NewAuth(AuthBearer).Normalized()
	if auth.Type != AuthBearer {
		t.Errorf("type = %q, want the one asked for", auth.Type)
	}
	if auth.Answer("prefix") != "Bearer" {
		t.Errorf("prefix = %q, want the scheme's own default", auth.Answer("prefix"))
	}
	if _, ok := auth.Fields["token"]; ok {
		t.Error("a field with no default belongs absent, not empty: empty is an answer nobody gave")
	}

	// Every choice of a select has a first one, or there is nothing to start on.
	for _, scheme := range AuthSchemes() {
		filled := NewAuth(scheme.Type).Normalized()
		for _, field := range scheme.Fields {
			if field.Kind == FieldSelect && filled.Answer(field.Key) == "" {
				t.Errorf("%s: select %q starts with nothing chosen", scheme.Type, field.Key)
			}
		}
	}
}

// With is a copy. Every holder of an Auth shares its map, and an edit that reached through it would
// change an authorization nobody meant to touch — a level's own while a request is being edited.
func TestWithDoesNotReachThrough(t *testing.T) {
	original := NewAuth(AuthBearer).With("token", "first")
	edited := original.With("token", "second")

	if original.Answer("token") != "first" {
		t.Errorf("original = %q, want it left alone", original.Answer("token"))
	}
	if edited.Answer("token") != "second" {
		t.Errorf("edited = %q, want the new answer", edited.Answer("token"))
	}
}

// The answers to the schemes that are not in use are kept: switching a request from Bearer to
// Basic and back is one gesture, and a token that did not survive it is one to paste again.
func TestAnswersToOtherSchemesAreKept(t *testing.T) {
	switched := Auth{Type: AuthBasic, Fields: map[string]string{
		"token": "left-over", "username": "user",
	}}
	normalized := switched.Normalized()

	if normalized.Answer("token") != "left-over" {
		t.Errorf("token = %q, want the answer kept for the scheme it belongs to",
			normalized.Answer("token"))
	}
	if normalized.Answer("username") != "user" {
		t.Errorf("username = %q, want the answer that was given", normalized.Answer("username"))
	}

	back := Auth{Type: AuthBearer, Fields: normalized.Fields}.Normalized()
	if back.Answer("token") != "left-over" {
		t.Errorf("token = %q, want what was there before the switch", back.Answer("token"))
	}

	// The one thing the window never sends is what a field starts at, because the window is not the
	// side that knows: a select has a first choice whether or not anyone picked one.
	filled := NewAuth(AuthAPIKey).Normalized()
	if filled.Answer("place") != "header" {
		t.Errorf("place = %q, want the scheme's own first choice", filled.Answer("place"))
	}
	// An answer already given wins over the default, including an empty one: a prefix somebody
	// cleared is a prefix they do not want.
	cleared := NewAuth(AuthBearer).With("prefix", "")
	if cleared.Normalized().Answer("prefix") != "" {
		t.Error("a prefix somebody cleared came back")
	}

	// A scheme nobody has heard of normalizes to nothing rather than to itself with its answers
	// kept: there is no field list to say what any of them mean.
	stranger := Auth{Type: "nobody-has-this", Fields: map[string]string{"token": "x"}}
	if len(stranger.Normalized().Fields) != 0 {
		t.Errorf("an unknown scheme kept %v", stranger.Normalized().Fields)
	}
}

func hasOption(options []Option, value string) bool {
	for _, option := range options {
		if option.Value == value {
			return true
		}
	}
	return false
}

// The example of RFC 7617 §2: Aladdin and "open sesame".
func TestBasicAuthorizationIsTheCredentialOfRFC7617(t *testing.T) {
	const want = "Basic QWxhZGRpbjpvcGVuIHNlc2FtZQ=="
	if got := BasicAuthorization("Aladdin", "open sesame"); got != want {
		t.Errorf("BasicAuthorization = %q, want %q", got, want)
	}
	user, password, ok := BasicCredentials(want)
	if !ok || user != "Aladdin" || password != "open sesame" {
		t.Errorf("BasicCredentials = %q, %q, %v", user, password, ok)
	}
}

func TestBasicCredentialsReadWhatTheSchemeAllows(t *testing.T) {
	for _, c := range []struct {
		header, user, password string
		ok                     bool
	}{
		{"basic QWxhZGRpbjpvcGVuIHNlc2FtZQ==", "Aladdin", "open sesame", true},
		{BasicAuthorization("me", "pa:ss"), "me", "pa:ss", true},
		{BasicAuthorization("me", ""), "me", "", true},
		{"Bearer QWxhZGRpbjpvcGVuIHNlc2FtZQ==", "", "", false},
		{"Basic not-base64!", "", "", false},
		{"Basic bm9jb2xvbg==", "", "", false},
		{"", "", "", false},
	} {
		user, password, ok := BasicCredentials(c.header)
		if user != c.user || password != c.password || ok != c.ok {
			t.Errorf("BasicCredentials(%q) = %q, %q, %v, want %q, %q, %v",
				c.header, user, password, ok, c.user, c.password, c.ok)
		}
	}
}
