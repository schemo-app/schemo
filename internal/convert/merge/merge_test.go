package merge_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"schemo/internal/convert/merge"
	"schemo/internal/convert/openapi"
	"schemo/internal/domain"
)

const shopV1 = `openapi: 3.0.3
info: {title: Shop API, version: "1", description: The shop.}
servers:
  - {url: "https://api.shop.test/v1", description: Production}
  - {url: "https://staging.shop.test/v1", description: Staging}
security: [{bearer: []}]
components:
  securitySchemes:
    bearer: {type: http, scheme: bearer}
paths:
  /products:
    get:
      summary: List products
      tags: [Products]
      parameters:
        - {name: page, in: query, required: true, schema: {type: integer, default: 1}}
        - {name: limit, in: query, schema: {type: integer, default: 20}}
        - {name: X-Trace, in: header, schema: {type: string}}
      responses: {"200": {description: ok}}
    post:
      summary: Create product
      tags: [Products]
      requestBody:
        content:
          application/json:
            schema: {type: object, properties: {title: {type: string}}}
      responses: {"201": {description: made}}
  /products/{id}:
    get:
      summary: Get product
      tags: [Products]
      parameters: [{name: id, in: path, required: true, schema: {type: string}}]
      responses: {"200": {description: ok}}
  /orders:
    get:
      summary: List orders
      tags: [Orders]
      responses: {"200": {description: ok}}
`

// with is a description with one part replaced, which has to be there.
func with(t *testing.T, spec, old, replacement string) string {
	t.Helper()
	if !strings.Contains(spec, old) {
		t.Fatalf("the description holds no %q", old)
	}
	return strings.Replace(spec, old, replacement, 1)
}

// collection is a linked collection as the app would hold it, with what the last update said.
type collection struct {
	t    *testing.T
	mine domain.Collection
	base *merge.Snapshot
	held []domain.Environment
	next int
}

func read(t *testing.T, spec string) domain.Document {
	t.Helper()
	document, err := openapi.Import([]byte(spec))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	return document
}

// imported is the collection a first import of the description makes, linked to it.
func imported(t *testing.T, spec string) *collection {
	t.Helper()
	document := read(t, spec)
	c := &collection{t: t}
	c.mine = c.saved(document.Collections[0], "")
	for _, draft := range document.Environments {
		c.held = append(c.held, domain.Environment{
			ID: c.id(), Name: draft.Name, Collections: map[string][]domain.Variable{c.mine.ID: draft.Blocks[0].Variables},
		})
	}
	base := merge.Linked(c.input(spec))
	c.base = &base
	return c
}

func (c *collection) id() string {
	c.next++
	return "id" + strconv.Itoa(c.next)
}

func (c *collection) saved(read domain.Collection, parent string) domain.Collection {
	out := read
	out.ID, out.ParentID = c.id(), parent
	out.Items, out.Children = []domain.CollectionNode{}, []domain.Collection{}
	for _, node := range read.Items {
		node.ID, node.CollectionID, node.Operation = c.id(), out.ID, ""
		out.Items = append(out.Items, node)
	}
	for _, child := range read.Children {
		out.Children = append(out.Children, c.saved(child, out.ID))
	}
	return out
}

func (c *collection) input(spec string) merge.Input {
	document := read(c.t, spec)
	return merge.Input{
		Base: c.base, Mine: c.mine, Theirs: document.Collections[0],
		Environments: document.Environments, Held: c.held, Block: c.mine.ID,
	}
}

func (c *collection) review(spec string) merge.Review {
	return merge.Compare(c.input(spec))
}

// defaults is every item the review offers checked, and every conflict left to the person.
func defaults(review merge.Review) merge.Choices {
	var out merge.Choices
	items := slices.Concat(review.Added, review.Changed, review.Removed, review.Environments)
	if review.Settings != nil {
		items = append(items, *review.Settings)
	}
	for _, item := range items {
		if item.Selected {
			out.Selected = append(out.Selected, item.ID)
		}
	}
	return out
}

// update applies the choices and writes the result into the collection, as the app does.
func (c *collection) update(spec string, choices merge.Choices) merge.Result {
	c.t.Helper()
	result := merge.Apply(c.input(spec), choices, c.id)
	for _, level := range result.Update.Levels {
		parent := c.level(&c.mine, level.ParentID)
		parent.Children = append(parent.Children, level)
	}
	for _, patch := range result.Update.Patches {
		level := c.level(&c.mine, patch.ID)
		level.Name, level.Description, level.Auth, level.Variables = patch.Name, patch.Description, patch.Auth, patch.Variables
	}
	for _, node := range result.Update.Requests {
		node.Operation = ""
		level := c.level(&c.mine, node.CollectionID)
		if at := slices.IndexFunc(level.Items, func(one domain.CollectionNode) bool { return one.ID == node.ID }); at >= 0 {
			level.Items[at] = node
		} else {
			level.Items = append(level.Items, node)
		}
	}
	for _, id := range result.Update.Deleted {
		c.remove(&c.mine, id)
	}
	for _, draft := range result.Environments {
		at := slices.IndexFunc(c.held, func(one domain.Environment) bool { return one.Name == draft.Name })
		if at < 0 {
			c.held = append(c.held, domain.Environment{ID: c.id(), Name: draft.Name, Collections: map[string][]domain.Variable{}})
			at = len(c.held) - 1
		}
		c.held[at].Collections[c.mine.ID] = draft.Blocks[0].Variables
	}
	c.base = &result.Snapshot
	return result
}

func (c *collection) level(from *domain.Collection, id string) *domain.Collection {
	if from.ID == id {
		return from
	}
	for at := range from.Children {
		if found := c.level(&from.Children[at], id); found != nil {
			return found
		}
	}
	return nil
}

func (c *collection) remove(from *domain.Collection, id string) {
	from.Items = slices.DeleteFunc(from.Items, func(one domain.CollectionNode) bool { return one.ID == id })
	for at := range from.Children {
		c.remove(&from.Children[at], id)
	}
}

func find(from *domain.Collection, name string) *domain.CollectionNode {
	for at := range from.Items {
		if from.Items[at].Name == name {
			return &from.Items[at]
		}
	}
	for at := range from.Children {
		if found := find(&from.Children[at], name); found != nil {
			return found
		}
	}
	return nil
}

func (c *collection) request(name string) *domain.CollectionNode {
	c.t.Helper()
	node := find(&c.mine, name)
	if node == nil {
		c.t.Fatalf("no request %q", name)
	}
	return node
}

func (c *collection) folder(name string) *domain.Collection {
	for at := range c.mine.Children {
		if c.mine.Children[at].Name == name {
			return &c.mine.Children[at]
		}
	}
	return nil
}

func row(rows []domain.Row, name string) *domain.Row {
	for at := range rows {
		if rows[at].Name == name {
			return &rows[at]
		}
	}
	return nil
}

func names(items []merge.Item) []string {
	out := []string{}
	for _, item := range items {
		out = append(out, item.Name)
	}
	return out
}

func changes(item merge.Item) []string {
	out := []string{}
	for _, change := range item.Changes {
		text := string(change.Field)
		if change.Name != "" {
			text += " " + change.Name
		}
		if change.Attribute != "" {
			text += " " + string(change.Attribute)
		}
		if change.Conflict {
			text += " !"
		}
		out = append(out, text)
	}
	return out
}

func nothingToApply(t *testing.T, review merge.Review) {
	t.Helper()
	if len(review.Added) != 0 || len(review.Changed) != 0 || len(review.Removed) != 0 ||
		review.Settings != nil || len(review.Environments) != 0 {
		t.Errorf("review = added %v, changed %v, removed %v, settings %v, environments %v; want nothing",
			names(review.Added), names(review.Changed), names(review.Removed), review.Settings,
			names(review.Environments))
		for _, item := range review.Changed {
			t.Errorf("  %s: %v", item.Name, changes(item))
		}
	}
}

func TestAnImportedCollectionMatchesItsDescription(t *testing.T) {
	c := imported(t, shopV1)

	review := c.review(shopV1)

	nothingToApply(t, review)
}

func TestThePersonsWorkIsLeftAlone(t *testing.T) {
	c := imported(t, shopV1)
	list := c.request("List products")
	list.Headers = append(list.Headers, domain.Row{Name: "X-Tenant", Value: "acme", Enabled: true})
	row(list.Params, "page").Value = "5"
	list.URL = "{{baseUrl}}/products?page=5"
	c.request("Create product").Body = `{"title":"lamp"}`
	c.request("Get product").Name = "Fetch one product"
	products := c.folder("Products")
	products.Items = append(products.Items, domain.CollectionNode{
		ID: "mine", CollectionID: products.ID, Name: "My smoke check", Method: "GET", URL: "{{baseUrl}}/health",
		Position: 9,
	})

	review := c.review(shopV1)

	nothingToApply(t, review)
	if result := c.update(shopV1, defaults(review)); len(result.Update.Requests)+len(result.Update.Patches) != 0 {
		t.Errorf("update = %+v, want nothing written", result.Update)
	}
}

func TestUntouchedFieldsFollowTheDescription(t *testing.T) {
	c := imported(t, shopV1)
	list := c.request("List products")
	list.Headers = append(list.Headers, domain.Row{Name: "X-Tenant", Value: "acme", Enabled: true})
	row(list.Params, "page").Value = "5"
	list.URL = "{{baseUrl}}/products?page=5"
	v2 := with(t, shopV1, "summary: List products", "summary: List all products\n      description: Every product.")
	v2 = with(t, v2, "- {name: X-Trace",
		"- {name: sort, in: query, required: true, schema: {type: string, default: name}}\n        - {name: X-Trace")

	review := c.review(v2)

	if len(review.Changed) != 1 || len(review.Added)+len(review.Removed) != 0 {
		t.Fatalf("review = %v / %v / %v, want one changed request", names(review.Added), names(review.Changed), names(review.Removed))
	}
	want := []string{"name", "description", "param sort"}
	if got := changes(review.Changed[0]); !slices.Equal(got, want) {
		t.Errorf("changes = %v, want %v and no conflict", got, want)
	}
	c.update(v2, defaults(review))
	list = c.request("List all products")
	if list.URL != "{{baseUrl}}/products?page=5&sort=name" || list.Description != "Every product." {
		t.Errorf("url = %q, description = %q; want the person's page beside the new sort", list.URL, list.Description)
	}
	if row(list.Headers, "X-Tenant") == nil || row(list.Params, "sort") == nil || row(list.Params, "page").Value != "5" {
		t.Errorf("rows = %+v %+v, want both sides", list.Params, list.Headers)
	}
	nothingToApply(t, c.review(v2))
}

func TestAConflictKeepsThePersonsSideUnlessChosen(t *testing.T) {
	edit := func() (*collection, string) {
		c := imported(t, shopV1)
		c.request("Create product").Body = `{"title":"lamp"}`
		return c, with(t, shopV1, "properties: {title: {type: string}}", "properties: {title: {type: string}, price: {type: integer}}")
	}

	c, v2 := edit()
	review := c.review(v2)
	if len(review.Changed) != 1 || !slices.Equal(changes(review.Changed[0]), []string{"body !"}) {
		t.Fatalf("changed = %v %v, want a body conflict", names(review.Changed), review.Changed)
	}
	c.update(v2, defaults(review))
	if body := c.request("Create product").Body; body != `{"title":"lamp"}` {
		t.Errorf("body = %s, want the person's", body)
	}
	nothingToApply(t, c.review(v2)) // decided: asked again only when the description moves again

	c, v2 = edit()
	choices := defaults(c.review(v2))
	choices.Theirs = []string{c.review(v2).Changed[0].Changes[0].ID}
	c.update(v2, choices)
	if body := c.request("Create product").Body; !strings.Contains(body, `"price"`) {
		t.Errorf("body = %s, want the description's", body)
	}
}

// Reformatting a JSON body is no edit of it.
func TestAReformattedBodyIsUntouched(t *testing.T) {
	c := imported(t, shopV1)
	c.request("Create product").Body = `{"title": "string"}`
	v2 := with(t, shopV1, "properties: {title: {type: string}}", "properties: {name: {type: string}}")

	review := c.review(v2)

	if len(review.Changed) != 1 || !slices.Equal(changes(review.Changed[0]), []string{"body"}) {
		t.Errorf("changed = %+v, want a plain body update", review.Changed)
	}
}

func TestAnUncheckedChangeIsOfferedAgain(t *testing.T) {
	c := imported(t, shopV1)
	v2 := with(t, shopV1, "summary: List orders", "summary: List all orders")

	c.update(v2, merge.Choices{})

	if review := c.review(v2); len(review.Changed) != 1 {
		t.Errorf("changed = %v, want the unchecked rename again", names(review.Changed))
	}
}

func TestNewOperationsLandInTheirFolders(t *testing.T) {
	c := imported(t, shopV1)
	v2 := shopV1 + `  /orders/{id}:
    get: {summary: Get order, tags: [Orders], parameters: [{name: id, in: path, required: true}], responses: {"200": {description: ok}}}
  /refunds:
    get: {summary: List refunds, tags: [Refunds], responses: {"200": {description: ok}}}
`

	review := c.review(v2)
	if got := names(review.Added); !slices.Equal(got, []string{"Get order", "List refunds"}) {
		t.Fatalf("added = %v", got)
	}
	result := c.update(v2, defaults(review))

	orders, refunds := c.folder("Orders"), c.folder("Refunds")
	if result.Added != 2 || orders == nil || len(orders.Items) != 2 || orders.Items[1].Position <= orders.Items[0].Position ||
		refunds == nil || len(refunds.Items) != 1 || refunds.ParentID != c.mine.ID {
		t.Errorf("tree = %+v, want the new ones in their folders", c.mine.Children)
	}
	nothingToApply(t, c.review(v2))
}

func TestADroppedOperationStaysUnlessChosen(t *testing.T) {
	c := imported(t, shopV1)
	v2 := with(t, shopV1, `  /orders:
    get:
      summary: List orders
      tags: [Orders]
      responses: {"200": {description: ok}}
`, "")

	review := c.review(v2)
	if len(review.Removed) != 1 || review.Removed[0].Selected || review.Removed[0].Edited {
		t.Fatalf("removed = %+v, want the untouched one unchecked", review.Removed)
	}
	c.update(v2, defaults(review))
	c.request("List orders").Headers = []domain.Row{{Name: "X-Mine", Value: "1", Enabled: true}}
	review = c.review(v2)
	if len(review.Removed) != 1 || !review.Removed[0].Edited {
		t.Fatalf("removed = %+v, want it still offered, now edited", review.Removed)
	}
	c.update(v2, merge.Choices{Selected: []string{review.Removed[0].ID}})
	if find(&c.mine, "List orders") != nil {
		t.Error("the chosen removal did not delete the request")
	}
	nothingToApply(t, c.review(v2))
}

func TestADeletedRequestDoesNotComeBackByItself(t *testing.T) {
	c := imported(t, shopV1)
	c.remove(&c.mine, c.request("Get product").ID)

	review := c.review(shopV1)
	if len(review.Added) != 1 || !review.Added[0].Declined || review.Added[0].Selected {
		t.Fatalf("added = %+v, want the deleted one offered unchecked", review.Added)
	}
	c.update(shopV1, defaults(review))
	if find(&c.mine, "Get product") != nil {
		t.Fatal("a deleted request came back")
	}
	c.update(shopV1, merge.Choices{Selected: []string{review.Added[0].ID}})
	if find(&c.mine, "Get product") == nil {
		t.Error("choosing it did not bring it back")
	}
}

func TestALinkWithNoHistoryOverwritesNothing(t *testing.T) {
	c := imported(t, shopV1)
	c.base = nil
	list := c.request("List products")
	row(list.Params, "page").Value = "7"
	list.URL = "{{baseUrl}}/products?page=7"
	c.request("Create product").Body = `{"title":"lamp"}`
	own := c.folder("Products")
	own.Items = append(own.Items, domain.CollectionNode{
		ID: "mine", CollectionID: own.ID, Name: "My smoke check", Method: "GET", URL: "{{baseUrl}}/health", Position: 9,
	})
	v2 := with(t, shopV1, "summary: List products", "summary: List all products")
	v2 = with(t, v2, "- {name: X-Trace",
		"- {name: sort, in: query, schema: {type: string}}\n        - {name: X-Trace")
	v2 += `  /refunds:
    get: {summary: List refunds, tags: [Refunds], responses: {"200": {description: ok}}}
`

	review := c.review(v2)
	if !slices.Equal(names(review.Added), []string{"List refunds"}) || len(review.Removed) != 0 {
		t.Fatalf("review = added %v, removed %v", names(review.Added), names(review.Removed))
	}
	c.update(v2, defaults(review))

	list = c.request("List products")
	if row(list.Params, "page").Value != "7" || row(list.Params, "sort") == nil || list.URL != "{{baseUrl}}/products?page=7" {
		t.Errorf("list = %+v, want the person's values and the new parameter", list)
	}
	if c.request("Create product").Body != `{"title":"lamp"}` || find(&c.mine, "My smoke check") == nil {
		t.Error("a first link overwrote the person's work")
	}
	// From the second update on, the past is known.
	nothingToApply(t, c.review(v2))
}

// With no history, a field the description leaves empty offers nothing to choose.
func TestAnEmptyFieldOfTheDescriptionIsNoDifference(t *testing.T) {
	c := imported(t, shopV1)
	c.base = nil
	c.request("List orders").Description = "Ours."
	row(c.request("List products").Headers, "X-Trace").Value = "abc"
	silent := with(t, shopV1, "security: [{bearer: []}]\n", "")
	silent = with(t, silent, `, description: The shop.`, "")

	nothingToApply(t, c.review(silent))
}

// Switching schemes leaves the old one's fields stored. They are not part of the authorization, and
// one of them may be a password in the clear.
func TestAFieldOfAnotherSchemeIsNeitherComparedNorShown(t *testing.T) {
	c := imported(t, shopV1)
	c.mine.Auth.Fields["password"] = "hunter2"

	review := c.review(with(t, shopV1, "security: [{bearer: []}]\n", ""))

	if review.Settings == nil || !slices.Equal(changes(*review.Settings), []string{"auth"}) {
		t.Fatalf("settings = %+v, want the description's change taken without a conflict", review.Settings)
	}
	if shown := review.Settings.Changes[0].Mine; strings.Contains(shown, "password") {
		t.Errorf("the review shows %q", shown)
	}
}

func TestARenamedPathParameterKeepsItsValue(t *testing.T) {
	c := imported(t, shopV1)
	c.request("Get product").PathParams[0].Value = "42"
	v2 := with(t, shopV1, "/products/{id}:", "/products/{productId}:")
	v2 = with(t, v2, "parameters: [{name: id, in: path", "parameters: [{name: productId, in: path")

	review := c.review(v2)
	if len(review.Added)+len(review.Removed) != 0 || len(review.Changed) != 1 {
		t.Fatalf("review = %v / %v / %v, want one changed request", names(review.Added), names(review.Changed), names(review.Removed))
	}
	c.update(v2, defaults(review))

	node := c.request("Get product")
	if node.URL != "{{baseUrl}}/products/:productId" || len(node.PathParams) != 1 ||
		node.PathParams[0].Name != "productId" || node.PathParams[0].Value != "42" {
		t.Errorf("node = %q %+v, want the value under the new name", node.URL, node.PathParams)
	}
}

func TestARenamedTagRenamesTheFolder(t *testing.T) {
	c := imported(t, shopV1)
	folder := c.folder("Orders").ID
	v2 := with(t, shopV1, "tags: [Orders]", "tags: [Purchases]")

	review := c.review(v2)
	if len(review.Added)+len(review.Removed)+len(review.Changed) != 0 || review.Settings == nil ||
		!slices.Equal(changes(*review.Settings), []string{"folder Purchases name"}) {
		t.Fatalf("review = %+v, want only the folder renamed", review.Settings)
	}
	c.update(v2, defaults(review))

	if renamed := c.folder("Purchases"); renamed == nil || renamed.ID != folder || len(c.mine.Children) != 2 {
		t.Errorf("folders = %+v, want the same folder renamed", c.mine.Children)
	}
	nothingToApply(t, c.review(v2))
}

func TestCollectionValuesFollowUntilThePersonSetsThem(t *testing.T) {
	c := imported(t, shopV1)
	for at := range c.mine.Variables {
		if c.mine.Variables[at].Name == "token" {
			c.mine.Variables[at].Value, c.mine.Variables[at].Enabled = "secret-token", true
		}
	}
	v2 := with(t, shopV1, "https://api.shop.test/v1", "https://api.shop.test/v2")

	review := c.review(v2)
	if review.Settings == nil || !slices.Equal(changes(*review.Settings), []string{"variable baseUrl value"}) {
		t.Fatalf("settings = %+v, want only the base address", review.Settings)
	}
	c.update(v2, defaults(review))

	values := map[string]string{}
	for _, one := range c.mine.Variables {
		values[one.Name] = one.Value
	}
	if values["baseUrl"] != "https://api.shop.test/v2" || values["token"] != "secret-token" {
		t.Errorf("variables = %v", values)
	}
}

func TestEnvironmentValuesFollowUntilThePersonSetsThem(t *testing.T) {
	c := imported(t, shopV1)
	staging := func() string { return c.held[0].Collections[c.mine.ID][0].Value }
	v2 := with(t, shopV1, "https://staging.shop.test/v1", "https://staging.shop.test/v2")

	review := c.review(v2)
	if len(review.Environments) != 1 || !slices.Equal(changes(review.Environments[0]), []string{"variable baseUrl value"}) {
		t.Fatalf("environments = %+v, want the staging address", review.Environments)
	}
	c.update(v2, defaults(review))
	if staging() != "https://staging.shop.test/v2" {
		t.Fatalf("staging = %q, want the new address", staging())
	}

	c.held[0].Collections[c.mine.ID][0].Value = "http://localhost:3000"
	v3 := with(t, v2, "https://staging.shop.test/v2", "https://staging.shop.test/v3")
	review = c.review(v3)
	if len(review.Environments) != 1 || !review.Environments[0].Changes[0].Conflict {
		t.Fatalf("environments = %+v, want a conflict", review.Environments)
	}
	c.update(v3, defaults(review))
	if staging() != "http://localhost:3000" {
		t.Errorf("staging = %q, want the person's address", staging())
	}
}

func TestAReadOnlyEnvironmentIsNotWritten(t *testing.T) {
	c := imported(t, shopV1)
	c.held[0].Readonly = true
	v2 := with(t, shopV1, "https://staging.shop.test/v1", "https://staging.shop.test/v2")

	review := c.review(v2)

	if len(review.Environments) != 0 {
		t.Errorf("review = %+v, want the environment left alone", review.Environments)
	}
	if result := c.update(v2, defaults(review)); len(result.Environments) != 0 {
		t.Errorf("update wrote %+v into a read-only environment", result.Environments)
	}
}
