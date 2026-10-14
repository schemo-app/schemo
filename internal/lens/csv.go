package lens

import (
	"fmt"
	"regexp"
	"strings"

	"schemo/internal/domain"
)

// parseCSV reads delimited text as rows: objects keyed by the header when there is one, arrays
// of cells when not. Every cell keeps its position.
func parseCSV(text string) (*node, error) {
	delimiter := delimiterOf(text)
	if delimiter == 0 {
		return nil, fmt.Errorf("%w: no delimiter", domain.ErrUnreadable)
	}
	rows := splitRows(text, delimiter)
	if len(rows) < 2 || len(rows[0]) < 2 {
		return nil, fmt.Errorf("%w: not a table", domain.ErrUnreadable)
	}

	// A table is rows of one width; text that merely has commas is not.
	width, agree := len(rows[0]), 0
	for _, row := range rows {
		if len(row) == width {
			agree++
		}
	}
	if agree*10 < len(rows)*7 {
		return nil, fmt.Errorf("%w: the rows differ in width", domain.ErrUnreadable)
	}

	header := headerOf(rows)
	root := &node{kind: domain.NodeArray, start: 0, end: len(text), keyStart: -1}
	for _, cells := range rows[boolToInt(header != nil):] {
		if len(cells) == 0 {
			continue
		}
		row := &node{
			kind: domain.NodeArray, parent: root, keyStart: -1,
			start: cells[0].start, end: cells[len(cells)-1].end,
		}
		if header != nil {
			row.kind = domain.NodeObject
		}
		for i, cell := range cells {
			kid := &node{
				kind: cell.kind(), parent: row, keyStart: -1, start: cell.start, end: cell.end,
				scalar: cell.text,
			}
			if header != nil {
				kid.name = columnName(header, i)
			}
			row.kids = append(row.kids, kid)
		}
		root.kids = append(root.kids, row)
	}
	if len(root.kids) == 0 {
		return nil, fmt.Errorf("%w: no rows", domain.ErrUnreadable)
	}
	uniform(root)
	if header != nil {
		root.header = header
	}
	return root, nil
}

// uniform gives a column one kind: a number only if every cell is one.
func uniform(root *node) {
	kinds := map[int]domain.NodeKind{}
	for _, row := range root.kids {
		for i, c := range row.kids {
			if have, ok := kinds[i]; ok && have != c.kind {
				kinds[i] = domain.NodeString
			} else if !ok {
				kinds[i] = c.kind
			}
		}
	}
	for _, row := range root.kids {
		for i, c := range row.kids {
			c.kind = kinds[i]
		}
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// cell is a field: its text unquoted, and the bytes it lies in, quotes included.
type cell struct {
	text       string
	start, end int
}

var (
	number  = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)
	boolean = regexp.MustCompile(`^(true|false)$`)
)

// kind is what a cell holds; a leading zero keeps it text.
func (c cell) kind() domain.NodeKind {
	switch {
	case number.MatchString(c.text):
		return domain.NodeNumber
	case boolean.MatchString(c.text):
		return domain.NodeBoolean
	}
	return domain.NodeString
}

// delimiterOf is the separator the first lines use most, or zero.
func delimiterOf(text string) byte {
	head := text
	if len(head) > 4096 {
		head = head[:4096]
	}
	best, most := byte(0), 0
	for _, candidate := range []byte{',', ';', '\t', '|'} {
		if count := strings.Count(head, string(candidate)); count > most {
			best, most = candidate, count
		}
	}
	return best
}

// splitRows reads the fields of each line; a quoted field may hold the delimiter, quotes and
// line breaks.
func splitRows(text string, delimiter byte) [][]cell {
	var rows [][]cell
	var row []cell
	i := 0
	for i <= len(text) {
		start := i
		var value strings.Builder
		if i < len(text) && text[i] == '"' {
			i++
			for i < len(text) {
				if text[i] == '"' {
					if i+1 < len(text) && text[i+1] == '"' {
						value.WriteByte('"')
						i += 2
						continue
					}
					i++
					break
				}
				value.WriteByte(text[i])
				i++
			}
			// Text after the closing quote belongs to the field.
			for i < len(text) && text[i] != delimiter && text[i] != '\n' && text[i] != '\r' {
				value.WriteByte(text[i])
				i++
			}
		} else {
			for i < len(text) && text[i] != delimiter && text[i] != '\n' && text[i] != '\r' {
				value.WriteByte(text[i])
				i++
			}
		}
		row = append(row, cell{text: strings.TrimSpace(value.String()), start: start, end: i})

		switch {
		case i >= len(text):
			i++
		case text[i] == delimiter:
			i++
			continue
		default:
			if text[i] == '\r' && i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			i++
		}
		if !(len(row) == 1 && row[0].text == "" && row[0].start == row[0].end) {
			rows = append(rows, row)
		}
		row = nil
	}
	if len(row) > 0 && !(len(row) == 1 && row[0].text == "") {
		rows = append(rows, row)
	}
	return rows
}

// headerOf is the first row when every cell is a distinct non-numeric word.
func headerOf(rows [][]cell) []string {
	seen := map[string]bool{}
	names := make([]string, 0, len(rows[0]))
	for _, c := range rows[0] {
		if c.text == "" || number.MatchString(c.text) || seen[c.text] {
			return nil
		}
		seen[c.text] = true
		names = append(names, c.text)
	}
	return names
}

// csvForms is a cell's address as a path and as a spreadsheet reference.
func (d *Document) csvForms(n *node, steps []step) []domain.PathForm {
	forms := []domain.PathForm{{Syntax: "JSONPath", Text: jsonPath(steps)}}
	if n.parent != nil && n.parent.parent != nil {
		forms = append(forms, domain.PathForm{
			Syntax: "Cell", Text: spreadsheet(n, d.root.header != nil),
		})
	}
	return forms
}

func columnName(header []string, i int) string {
	if i < len(header) {
		return header[i]
	}
	return fmt.Sprintf("column%d", i+1)
}

// spreadsheet is a cell's reference like "C3"; a header counts as the first row.
func spreadsheet(n *node, header bool) string {
	row := n.parent.index() + 1
	if header {
		row++
	}
	column := n.index()
	letters := ""
	for c := column; c >= 0; c = c/26 - 1 {
		letters = string(rune('A'+c%26)) + letters
	}
	return fmt.Sprintf("%s%d", letters, row)
}
