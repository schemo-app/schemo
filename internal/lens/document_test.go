package lens

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"schemo/internal/domain"
)

const article = `{
  "data": [
    {"id": "1", "type": "articles", "attributes": {"title": "Héllo", "content-type": "x"}},
    {"id": "2", "type": "articles", "attributes": {"title": "Second"}}
  ],
  "meta": {"total": 2, "ok": true, "none": null}
}`

func mustOpen(t *testing.T, contentType, text string) *Document {
	t.Helper()
	doc, err := Open(contentType, text)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return doc
}

func TestAClickOnAValueNamesItsAddress(t *testing.T) {
	doc := mustOpen(t, "application/json", article)

	offset := indexOf(article, `"Second"`) + 2
	view := doc.At(offset)
	if view.Path != "$.data[1].attributes.title" {
		t.Errorf("path = %q, want the second article's title", view.Path)
	}
	if view.Kind != domain.NodeString || view.Preview != "Second" {
		t.Errorf("view = %+v, want the string Second", view)
	}
	if view.Parent != "$.data[1].attributes" {
		t.Errorf("parent = %q, want the attributes object", view.Parent)
	}
}

func TestCrumbsGoDownFromTheRootWithTheNodeLast(t *testing.T) {
	doc := mustOpen(t, "application/json", article)

	view := doc.At(indexOf(article, `"Second"`) + 2)
	want := []string{"", "data", "[1]", "attributes", "title"}
	if len(view.Crumbs) != len(want) {
		t.Fatalf("crumbs = %+v, want %d steps", view.Crumbs, len(want))
	}
	for i, name := range want {
		if view.Crumbs[i].Name != name {
			t.Errorf("crumbs[%d].Name = %q, want %q", i, view.Crumbs[i].Name, name)
		}
	}
	if last := view.Crumbs[len(view.Crumbs)-1]; last.Path != view.Path {
		t.Errorf("last crumb path = %q, want %q", last.Path, view.Path)
	}
}

func TestChildrenListsAContainersMembersWithoutOpeningThem(t *testing.T) {
	doc := mustOpen(t, "application/json", article)

	view, err := doc.Node("$.data[0]")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if len(view.Children) != 3 {
		t.Fatalf("children = %+v, want 3 members", view.Children)
	}
	if view.Children[0].Name != "id" || view.Children[0].Preview != `1` {
		t.Errorf("children[0] = %+v, want id 1", view.Children[0])
	}
	if view.Children[2].Preview != "{2}" {
		t.Errorf("children[2].Preview = %q, want the attributes' size", view.Children[2].Preview)
	}
}

func TestAClickOnAKeyIsAClickOnItsValue(t *testing.T) {
	doc := mustOpen(t, "application/json", article)

	view := doc.At(indexOf(article, `"total"`) + 2)
	if view.Path != "$.meta.total" || view.Kind != domain.NodeNumber {
		t.Errorf("view = %+v, want the number meta.total", view)
	}
}

func TestAKeyThatIsNotAWordIsQuotedInEveryNotation(t *testing.T) {
	doc := mustOpen(t, "application/json", article)

	view, err := doc.Node("$.data[0].attributes['content-type']")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	want := map[string]string{
		"JSONPath":   "$.data[0].attributes['content-type']",
		"jq":         `.data[0].attributes.["content-type"]`,
		"JavaScript": `body.data[0].attributes["content-type"]`,
		"Python":     `body["data"][0]["attributes"]["content-type"]`,
		"Postman":    `pm.response.json().data[0].attributes["content-type"]`,
	}
	for _, form := range view.Forms {
		if form.Text != want[form.Syntax] {
			t.Errorf("%s = %q, want %q", form.Syntax, form.Text, want[form.Syntax])
		}
	}
}

func TestTheOffsetsAreTheEditorsUnits(t *testing.T) {
	// "Héllo" holds one two-byte character and "😀" one of four bytes and two UTF-16 units.
	text := `{"a": "Hé😀", "b": "x"}`
	doc := mustOpen(t, "application/json", text)

	// One unit more than the runes before it: the emoji is two.
	b := doc.At(len([]rune(`{"a": "Hé😀", `)) + 2)
	if b.Path != "$.b" {
		t.Errorf("path = %q, want $.b", b.Path)
	}
	if b.Start != 19 || b.End != 22 {
		t.Errorf("range = %d..%d, want 19..22 in UTF-16 units", b.Start, b.End)
	}
}

func TestAPathTheTreeWritesIsRead(t *testing.T) {
	doc := mustOpen(t, "application/json", article)

	view, err := doc.Node("data[1].attributes.title")
	if err != nil || view.Preview != "Second" {
		t.Errorf("Node = %+v, %v, want the title", view, err)
	}
	if _, err := doc.Node("$.data[7]"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a position past the end = %v, want ErrNotFound", err)
	}
}

func TestATextThatIsNotADocumentIsUnreadable(t *testing.T) {
	for _, text := range []string{"plain words", `{"a": }`, `{"a": 1} trailing`, ""} {
		if _, err := Open("application/json", text); !errors.Is(err, domain.ErrUnreadable) {
			t.Errorf("Open(%q) = %v, want ErrUnreadable", text, err)
		}
	}
}

func TestAnElementIsAddressedByItsPlaceAmongItsSiblings(t *testing.T) {
	const feed = `<?xml version="1.0"?>
<feed xmlns="http://www.w3.org/2005/Atom" id="f">
  <entry><title>One</title></entry>
  <entry lang="ru"><title>Two</title><link href="http://x/2"/></entry>
</feed>`
	doc := mustOpen(t, "application/atom+xml", feed)

	view := doc.At(indexOf(feed, "Two") + 1)
	if view.Path != "/feed/entry[2]/title/text()" && view.Path != "/feed/entry[2]/title" {
		t.Errorf("path = %q, want the second entry's title", view.Path)
	}

	attr, err := doc.Node("/feed/entry[2]/link/@href")
	if err != nil || attr.Preview != "http://x/2" || attr.Kind != domain.NodeAttribute {
		t.Errorf("Node = %+v, %v, want the href attribute", attr, err)
	}
	onAttr := doc.At(indexOf(feed, `lang="ru"`) + 2)
	if onAttr.Path != "/feed/entry[2]/@lang" {
		t.Errorf("a click on an attribute = %q, want /feed/entry[2]/@lang", onAttr.Path)
	}
}

func indexOf(text, part string) int {
	for i := 0; i+len(part) <= len(text); i++ {
		if text[i:i+len(part)] == part {
			return len([]rune(text[:i])) // runes are enough while the fixtures hold no astral text
		}
	}
	return -1
}

func TestACopyOfAScalarIsItsValueAndOfAContainerIsItsText(t *testing.T) {
	doc := mustOpen(t, "application/json", `{"a": "x\ny", "b": {"c": 1}}`)

	if got, _ := doc.Text("$.a"); got != "x\ny" {
		t.Errorf("a string copies as %q, want it unquoted and unescaped", got)
	}
	if got, _ := doc.Text("$.b"); got != `{"c": 1}` {
		t.Errorf("an object copies as %q, want its text", got)
	}
}

func TestAMemberBeginsAtItsKey(t *testing.T) {
	doc := mustOpen(t, "application/json", `{"ab": [1, 2]}`)

	member, _ := doc.Node("$.ab")
	if member.Head != 1 || member.Start != 7 {
		t.Errorf("member = %d, %d, want the key at 1 and the value at 7", member.Head, member.Start)
	}
	item, _ := doc.Node("$.ab[1]")
	if item.Head != item.Start {
		t.Errorf("an array item begins at %d, want its own start %d", item.Head, item.Start)
	}
}

func TestANodeBecomesARequestBodyWithoutWhatTheServerOwns(t *testing.T) {
	doc := mustOpen(t, "application/json", `{"data": {"id": 7, "name": "Ann",
  "created_at": "2024-01-01", "tags": ["a"], "links": {"self": "x"},
  "owner": {"id": 1, "updatedAt": "y", "n": 2}}}`)

	whole, err := doc.Body("$.data", false)
	if err != nil {
		t.Fatalf("Body: %v", err)
	}
	if !strings.Contains(whole, `"created_at": "2024-01-01"`) || !strings.Contains(whole, `"links"`) {
		t.Errorf("a whole copy lost members:\n%s", whole)
	}

	bare, err := doc.Body("$.data", true)
	if err != nil {
		t.Fatalf("Body: %v", err)
	}
	want := `{
  "id": 7,
  "name": "Ann",
  "tags": [
    "a"
  ],
  "owner": {
    "id": 1,
    "n": 2
  }
}`
	if bare != want {
		t.Errorf("bare body =\n%s\nwant\n%s", bare, want)
	}
	if !json.Valid([]byte(bare)) {
		t.Error("the body is not JSON")
	}
}

const page = `<!DOCTYPE html>
<html lang="en"><head><title>Shop</title></head>
<body>
  <ul id="menu"><li><a href="/a">First</a><li><a href="/b" class=hot disabled>Second</a></ul>
  <p>One<p>Two <b>bold</b>
  <img src="x.png">
  <div class="card"><span>Price</span> 5</div><div class="card"><span>Price</span> 7</div>
</body></html>`

func TestAPageIsReadAsABrowserWouldReadIt(t *testing.T) {
	doc := mustOpen(t, "text/html; charset=utf-8", page)
	if doc.Kind() != domain.DocHTML {
		t.Fatalf("kind = %q, want html", doc.Kind())
	}

	// `li` ends `li`, and a block ends a paragraph: two items and two paragraphs, not a staircase.
	second, err := doc.Node("/html/body/ul/li[2]/a")
	if err != nil || second.Preview != "Second" {
		t.Fatalf("second link = %+v, %v", second, err)
	}
	forms := map[string]string{}
	for _, f := range second.Forms {
		forms[f.Syntax] = f.Text
	}
	if forms["CSS"] != "#menu > li:nth-of-type(2) > a" ||
		forms["JavaScript"] != `document.querySelector("#menu > li:nth-of-type(2) > a")` {
		t.Errorf("forms = %+v, want an address from the nearest id", forms)
	}

	// A click on an unquoted attribute and on one with no value both find the attribute.
	class := doc.At(indexOf(page, "hot") + 1)
	if class.Path != "/html/body/ul/li[2]/a/@class" || class.Preview != "hot" {
		t.Errorf("class = %+v, want the attribute", class)
	}
	disabled := doc.At(indexOf(page, "disabled") + 1)
	if disabled.Path != "/html/body/ul/li[2]/a/@disabled" {
		t.Errorf("disabled = %q, want the attribute", disabled.Path)
	}

	cards, _ := doc.Node("/html/body/div[2]/span")
	if cards.Preview != "Price" || cards.Path != "/html/body/div[2]/span" {
		t.Errorf("card = %+v, want the second card's label", cards)
	}
	if p, err := doc.Node("/html/body/p[2]"); err != nil || p.Count != 3 {
		t.Errorf("second paragraph = %+v, %v, want its text, the bold and the image", p, err)
	}
}

const table = "id,name,email,zip\n1,Ann,ann@x.io,01234\n2,\"Bob, Jr.\",bob@x.io,90210\n"

func TestATableIsReadAsRowsKeyedByItsHeader(t *testing.T) {
	doc := mustOpen(t, "text/csv", table)

	name, err := doc.Node("$[1].name")
	if err != nil || name.Preview != "Bob, Jr." {
		t.Fatalf("name = %+v, %v, want the quoted cell whole", name, err)
	}
	cell := ""
	for _, f := range name.Forms {
		if f.Syntax == "Cell" {
			cell = f.Text
		}
	}
	if cell != "C3" && cell != "B3" {
		t.Errorf("cell = %q, want the spreadsheet place of the second row's name", cell)
	}

	onZip := doc.At(indexOf(table, "01234") + 1)
	if onZip.Path != "$[0].zip" || onZip.Kind != domain.NodeString {
		t.Errorf("zip = %+v, want text: a leading zero is not a number", onZip)
	}
	if id, _ := doc.Node("$[1].id"); id.Kind != domain.NodeNumber {
		t.Errorf("id = %+v, want a number", id)
	}

	types, err := doc.Types("$", domain.TypeTypeScript)
	if err != nil || !strings.Contains(types, "id: number;") ||
		!strings.Contains(types, "zip: string;") {
		t.Errorf("types = %q, %v, want the columns typed by what they hold", types, err)
	}
}

func TestTextWithCommasIsNotATable(t *testing.T) {
	for _, text := range []string{
		"hello, world", "a,b\nplain sentence here\nanother one, with a comma, or two",
	} {
		if _, err := Open("text/csv", text); !errors.Is(err, domain.ErrUnreadable) {
			t.Errorf("Open(%q) = %v, want ErrUnreadable", text, err)
		}
	}
}
