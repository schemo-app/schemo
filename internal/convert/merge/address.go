package merge

import (
	"fmt"
	"slices"
	"strings"

	"schemo/internal/domain"
)

// composeURL is the address after the merge. A side keeps its own spelling when both the base and
// the enabled rows are its own; otherwise the query is written from the rows, as the editor does.
func composeURL(mine, theirs domain.CollectionNode, base string, params []domain.Row) string {
	mineBase, mineQuery := domain.SplitQuery(mine.URL)
	theirBase, _ := domain.SplitQuery(theirs.URL)
	rows := enabledPairs(params)
	switch {
	case base == mineBase && slices.Equal(rows, enabledPairs(mine.Params)):
		return mine.URL
	case base == theirBase && slices.Equal(rows, enabledPairs(theirs.Params)):
		return theirs.URL
	case slices.Equal(rows, enabledPairs(mine.Params)):
		return withQuery(base, mineQuery)
	}
	parts := make([]string, 0, len(rows))
	for _, pair := range rows {
		parts = append(parts, encodeQuery(pair.Name)+"="+encodeQuery(pair.Value))
	}
	// What the address carries and no row lists is kept as written.
	for _, part := range strings.Split(mineQuery, "&") {
		if part == "" {
			continue
		}
		name, _, _ := strings.Cut(part, "=")
		decoded := domain.QueryPair{Name: name}.Decoded().Name
		if !slices.ContainsFunc(mine.Params, func(row domain.Row) bool { return row.Name == name || row.Name == decoded }) {
			parts = append(parts, part)
		}
	}
	return withQuery(base, strings.Join(parts, "&"))
}

func enabledPairs(rows []domain.Row) []domain.QueryPair {
	var out []domain.QueryPair
	for _, row := range rows {
		if name := strings.TrimSpace(row.Name); row.Enabled && name != "" {
			out = append(out, domain.QueryPair{Name: name, Value: row.Value})
		}
	}
	return out
}

func withQuery(base, query string) string {
	if query == "" {
		return base
	}
	if hash := strings.IndexByte(base, '#'); hash >= 0 {
		return base[:hash] + "?" + query + base[hash:]
	}
	return base + "?" + query
}

// encodeQuery mirrors encodeQuery in frontend/src/lib/draftRows.ts.
func encodeQuery(text string) string {
	var out strings.Builder
	for _, b := range []byte(text) {
		switch {
		case 'a' <= b && b <= 'z', 'A' <= b && b <= 'Z', '0' <= b && b <= '9',
			strings.IndexByte("*-._~,[]", b) >= 0:
			out.WriteByte(b)
		case b == ' ':
			out.WriteByte('+')
		default:
			fmt.Fprintf(&out, "%%%02X", b)
		}
	}
	return strings.NewReplacer("%7B%7B", "{{", "%7D%7D", "}}").Replace(out.String())
}
