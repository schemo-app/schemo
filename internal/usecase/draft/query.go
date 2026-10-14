package draft

import (
	"net/url"
	"strings"

	"schemo/internal/domain"
)

// queryOf splits a URL into its base and its query string. The fragment stays with the base: it is
// not a parameter, and a URL that has one must survive a round trip through the parameter list.
func queryOf(raw string) (base string, query string) {
	at := strings.IndexByte(raw, '?')
	if at < 0 {
		return raw, ""
	}
	if hash := strings.IndexByte(raw[at:], '#'); hash >= 0 {
		return raw[:at] + raw[at+hash:], raw[at+1 : at+hash]
	}
	return raw[:at], raw[at+1:]
}

// paramsFromURL reads the query string into rows. A query string is the one place a value is
// percent-encoded, so this is where the decoding happens — the rows hold what the user typed.
func paramsFromURL(raw string) []domain.Row {
	_, query := queryOf(raw)
	if query == "" {
		return nil
	}

	out := []domain.Row{}
	for _, pair := range strings.Split(query, "&") {
		if pair == "" {
			continue
		}
		name, value, _ := strings.Cut(pair, "=")
		decodedName, err := url.QueryUnescape(name)
		if err != nil {
			decodedName = name
		}
		decodedValue, err := url.QueryUnescape(value)
		if err != nil {
			decodedValue = value
		}
		out = append(out, domain.Row{Name: decodedName, Value: decodedValue, Enabled: true})
	}
	return out
}

// joinURL is the other direction, and it is written by hand rather than with url.Values.Encode:
// that one sorts the names and escapes the braces of a `{{token}}`, and a URL the user typed has
// to come back the way it was, in the order it was written.
func joinURL(base string, rows []domain.Row) string {
	var query strings.Builder
	first := true
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if !row.Enabled || name == "" {
			continue
		}
		if !first {
			query.WriteByte('&')
		}
		first = false
		query.WriteString(encodeQuery(name))
		query.WriteByte('=')
		query.WriteString(encodeQuery(row.Value))
	}
	if query.Len() == 0 {
		return base
	}
	return base + "?" + query.String()
}

// Written by hand rather than taken from url.QueryEscape: both it and the browser escape `,` and
// the brackets, and this app's URLs are `include=author,comments` and `filter[id][in]=1,2`. All
// three are legal in a query and stay as typed; the rest follows the standard rule, space as `+`.
func encodeQuery(text string) string {
	var out strings.Builder
	out.Grow(len(text))

	for i := 0; i < len(text); i++ {
		switch c := text[i]; {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9',
			c == '*', c == '-', c == '.', c == '_', c == '~', c == ',', c == '[', c == ']':
			out.WriteByte(c)
		case c == ' ':
			out.WriteByte('+')
		default:
			out.WriteString("%")
			out.WriteByte("0123456789ABCDEF"[c>>4])
			out.WriteByte("0123456789ABCDEF"[c&0x0f])
		}
	}

	// The braces are put back last: `{{name}}` is what the user typed and what the resolver looks
	// for, and `%7B%7Bname%7D%7D` is a token nobody would recognise.
	escaped := out.String()
	escaped = strings.ReplaceAll(escaped, "%7B%7B", "{{")
	return strings.ReplaceAll(escaped, "%7D%7D", "}}")
}

// reconcile keeps the identity of the rows a text edit left standing. A row that is still in the
// URL keeps its id — an open popover, a focused input and a pending patch all point at ids — and
// the enabled flag goes with it.
//
// Rows the text no longer mentions are kept when they are switched off: a disabled parameter is
// not part of the URL, so a single keystroke in the URL field would otherwise throw away rows the
// user deliberately parked.
func reconcile(parsed []domain.Row, existing []domain.Row) []domain.Row {
	used := make([]bool, len(existing))
	out := make([]domain.Row, 0, len(parsed)+len(existing))

	for _, row := range parsed {
		at := -1
		for i, old := range existing {
			if !used[i] && old.Name == row.Name && old.Value == row.Value {
				at = i
				break
			}
		}
		if at < 0 {
			out = append(out, row)
			continue
		}
		used[at] = true
		row.ID = existing[at].ID
		out = append(out, row)
	}

	for i, old := range existing {
		if !used[i] && !old.Enabled {
			out = append(out, old)
		}
	}
	return out
}

// rowsFromURL reads the query string into rows and keeps what the previous set of rows contributes:
// the ids of the ones that stayed, and the ones switched off, which the URL does not mention.
func (u *UseCase) rowsFromURL(raw string, existing []domain.Row) []domain.Row {
	rows := reconcile(paramsFromURL(raw), existing)
	for i := range rows {
		if rows[i].ID == "" {
			rows[i].ID = u.ids()
		}
	}
	return rows
}

func syncURL(d *domain.Draft) {
	base, _ := queryOf(d.URL)
	d.URL = joinURL(base, d.Params)
}
