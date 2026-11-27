package app

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"schemo/internal/convert/merge"
	"schemo/internal/domain"
)

const linkedSpec = `openapi: 3.0.3
info: {title: Shop API, version: "1"}
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
        - {name: q, in: query, schema: {type: string}}
        - {name: Accept-Language, in: header, schema: {type: string, default: en}}
      responses:
        "200": {description: ok, content: {application/json: {schema: {type: array, items: {type: string}}}}}
    post:
      summary: Create product
      tags: [Products]
      security: []
      requestBody: {content: {application/json: {schema: {type: object, properties: {title: {type: string}}}}}}
      responses: {"201": {description: made}}
  /upload:
    post:
      summary: Upload file
      requestBody:
        content:
          multipart/form-data:
            schema: {type: object, properties: {file: {type: string, format: binary}, note: {type: string}}}
  /products/{id}/images/{image}.png:
    get:
      summary: Get image
      parameters:
        - {name: id, in: path, required: true, schema: {type: string}}
        - {name: image, in: path, required: true, schema: {type: string}}
`

// walkRequests visits every request of a collection read whole.
func walkRequests(collection domain.Collection, visit func(domain.CollectionNode)) {
	for _, node := range collection.Items {
		visit(node)
	}
	for _, child := range collection.Children {
		walkRequests(child, visit)
	}
}

func requestNamed(t *testing.T, d *device, collectionID, name string) domain.CollectionNode {
	t.Helper()
	full, err := d.Collections.Full(t.Context(), collectionID)
	if err != nil {
		t.Fatal(err)
	}
	var found *domain.CollectionNode
	walkRequests(full, func(node domain.CollectionNode) {
		if node.Name == name {
			found = &node
		}
	})
	if found == nil {
		t.Fatalf("no request %q", name)
	}
	return *found
}

func importSpec(t *testing.T, d *device, path string) domain.Collection {
	t.Helper()
	preview, err := d.Transfer.PreviewPath(t.Context(), "openapi", path)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := d.Transfer.Apply(t.Context(), preview.Token, "", "", "(imported)")
	if err != nil {
		t.Fatal(err)
	}
	return summary.Tree[len(summary.Tree)-1]
}

func chosen(review merge.Review) merge.Choices {
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

func describe(review merge.Review) string {
	var out strings.Builder
	items := slices.Concat(review.Added, review.Changed, review.Removed, review.Environments)
	if review.Settings != nil {
		items = append(items, *review.Settings)
	}
	for _, item := range items {
		out.WriteString(item.ID)
		for _, change := range item.Changes {
			out.WriteString(" [" + string(change.Field) + " " + change.Name + " " + string(change.Attribute) +
				": " + change.Mine + " -> " + change.Theirs + "]")
		}
		out.WriteString("\n")
	}
	return out.String()
}

// What the description said travels with the collection: another member updating it from their own
// copy gets the importer's review, and the importer then reviews against what the member applied.
func TestAMemberUpdatesWithTheImportersHistory(t *testing.T) {
	t.Parallel()
	cloud := newFakeCloud(t)
	ctx := t.Context()
	owner, member := newDevice(t), newDevice(t)
	owner.signIn(cloud, "ann")

	ownerFile := filepath.Join(t.TempDir(), "shop.yaml")
	writeImportFile(t, ownerFile, linkedSpec)
	collection := importSpec(t, owner, ownerFile)
	// The owner's own work: a request deleted on purpose, and a value and a token typed by hand.
	if err := owner.Collections.Delete(ctx, requestNamed(t, owner, collection.ID, "Upload file").ID); err != nil {
		t.Fatal(err)
	}
	editor, err := owner.Drafts.OpenNode(ctx, requestNamed(t, owner, collection.ID, "List products").ID)
	if err != nil {
		t.Fatal(err)
	}
	editor.Draft.Params[0].Value = "5"
	editor.Draft.Auth = domain.Auth{Type: domain.AuthBearer, Fields: map[string]string{"token": "typed-secret-token"}}
	if _, err := owner.Drafts.SaveNode(ctx, editor.Draft); err != nil {
		t.Fatal(err)
	}
	owner.sync()

	cloud.mu.Lock()
	for _, space := range cloud.workspaces {
		space.members["bob"] = true
	}
	cloud.mu.Unlock()
	member.signIn(cloud, "bob")
	member.sync()
	shared, err := member.Collections.WorkspaceOf(ctx, collection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.Workspaces.Switch(ctx, shared); err != nil {
		t.Fatal(err)
	}
	if source, err := member.Specs.Linked(ctx, collection.ID); err != nil || source != nil {
		t.Fatalf("Linked on another device = %+v, %v, want nothing to read from yet", source, err)
	}

	// The member's copy of the project: a request renamed, one gone and one new.
	renamed := strings.Replace(linkedSpec, "summary: List products", "summary: List all products", 1)
	second := renamed[:strings.Index(renamed, "  /products/{id}/images/")] +
		"  /refunds:\n    get: {summary: List refunds, responses: {\"200\": {description: ok}}}\n"
	memberFile := filepath.Join(t.TempDir(), "shop.yaml")
	writeImportFile(t, memberFile, second)
	review, err := member.Specs.ReviewFile(ctx, collection.ID, memberFile)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := offered(review.Review), []string{
		"add List refunds [x]", "add Upload file [ ]", "change List products: name", "remove Get image [ ]",
	}; !slices.Equal(got, want) {
		t.Fatalf("the member is offered %q, want %q", got, want)
	}
	if _, err := member.Specs.Apply(ctx, review.Token, chosen(review.Review)); err != nil {
		t.Fatal(err)
	}
	member.sync()
	owner.sync()

	// The owner's file moves on: the name the member just took changes again.
	writeImportFile(t, ownerFile, strings.Replace(second, "summary: List all products", "summary: List every product", 1))
	review, err = owner.Specs.ReviewLinked(ctx, collection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := offered(review.Review), []string{
		"add Upload file [ ]", "change List all products: name", "remove Get image [ ]",
	}; !slices.Equal(got, want) {
		t.Errorf("the owner is offered %q, want %q", got, want)
	}
	if _, err := owner.Specs.Apply(ctx, review.Token, chosen(review.Review)); err != nil {
		t.Fatal(err)
	}
	list := requestNamed(t, owner, collection.ID, "List every product")
	if list.Params[0].Value != "5" || list.Auth == nil || list.Auth.Fields["token"] != "typed-secret-token" {
		t.Errorf("after both updates the request holds %+v, %+v, want the typed value and token", list.Params, list.Auth)
	}
	// The snapshot syncs, outside the request's secret fields: nothing typed by hand may be in it.
	kept, err := owner.Store.SpecSnapshot(ctx, collection.ID)
	if err != nil || len(kept) == 0 || strings.Contains(string(kept), "typed-secret-token") {
		t.Errorf("snapshot = %d bytes, %v, want what the description said and nothing else", len(kept), err)
	}
}

// offered is a review as one line per row, a conflicting change marked.
func offered(review merge.Review) []string {
	out := []string{}
	mark := map[bool]string{true: "[x]", false: "[ ]"}
	for _, item := range review.Added {
		out = append(out, "add "+item.Name+" "+mark[item.Selected])
	}
	for _, item := range review.Removed {
		out = append(out, "remove "+item.Name+" "+mark[item.Selected])
	}
	items := slices.Concat(review.Changed, review.Environments)
	if review.Settings != nil {
		items = append(items, *review.Settings)
	}
	for _, item := range items {
		fields := []string{}
		for _, change := range item.Changes {
			field := string(change.Field)
			if change.Conflict {
				field += " (conflict)"
			}
			fields = append(fields, field)
		}
		out = append(out, "change "+item.Name+": "+strings.Join(fields, ", "))
	}
	slices.Sort(out)
	return out
}

// Saving a request from the editor unchanged must not look like an edit of the person's.
func TestSavingARequestUnchangedIsNoEdit(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "shop.yaml")
	writeImportFile(t, path, linkedSpec)
	d := newDevice(t)
	ctx := t.Context()
	collection := importSpec(t, d, path)
	before, err := d.Collections.Full(ctx, collection.ID)
	if err != nil {
		t.Fatal(err)
	}
	walkRequests(before, func(node domain.CollectionNode) {
		editor, err := d.Drafts.OpenNode(ctx, node.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Drafts.SaveNode(ctx, editor.Draft); err != nil {
			t.Fatal(err)
		}
	})
	after, err := d.Collections.Full(ctx, collection.ID)
	if err != nil {
		t.Fatal(err)
	}

	// A review does not show an edit the description leaves uncontested, so the two reads are
	// compared with each other: both ways, since with no history an empty side is passed over.
	for _, in := range []merge.Input{{Mine: before, Theirs: after}, {Mine: after, Theirs: before}} {
		if got := describe(merge.Compare(in)); got != "" {
			t.Errorf("the save changed:\n%s", got)
		}
	}
	review, err := d.Specs.ReviewLinked(ctx, collection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := describe(review.Review); got != "" {
		t.Errorf("review:\n%s", got)
	}
}

func TestARegeneratedDescriptionUpdatesAroundThePersonsWork(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "shop.yaml")
	writeImportFile(t, path, linkedSpec)
	d := newDevice(t)
	ctx := t.Context()
	collection := importSpec(t, d, path)

	linked, err := d.Specs.Linked(ctx, collection.ID)
	if err != nil || linked == nil || linked.Kind != domain.SpecFile || linked.Location != path {
		t.Fatalf("Linked = %+v, %v, want the imported file", linked, err)
	}

	// The person adds a header and sets a value in the editor, and keeps a secret in Staging.
	editor, err := d.Drafts.OpenNode(ctx, requestNamed(t, d, collection.ID, "List products").ID)
	if err != nil {
		t.Fatal(err)
	}
	draft := editor.Draft
	draft.Headers = append(draft.Headers, domain.Row{ID: "tenant", Name: "X-Tenant", Value: "acme", Enabled: true})
	for index := range draft.Params {
		if draft.Params[index].Name == "page" {
			draft.Params[index].Value = "5"
		}
	}
	draft.URL = "{{baseUrl}}/products?page=5"
	if _, err := d.Drafts.SaveNode(ctx, draft); err != nil {
		t.Fatal(err)
	}
	state, err := d.Environments.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	staging := state.Environments[slices.IndexFunc(state.Environments, func(one domain.Environment) bool {
		return one.Name == "Staging"
	})]
	block := domain.EnvScope{Environment: staging.ID, Collection: collection.ID}
	if _, err := d.Environments.SetInScope(ctx, block, "apiToken", "s3cret", true); err != nil {
		t.Fatal(err)
	}

	// The project regenerates its description.
	next := strings.Replace(linkedSpec, "summary: List products", "summary: List all products", 1)
	next = strings.Replace(next, "https://staging.shop.test/v1", "https://staging.shop.test/v2", 1)
	next = strings.Replace(next, "- {name: q, in: query", "- {name: sort, in: query, required: true, schema: {type: string, default: name}}\n        - {name: q, in: query", 1)
	next += `  /refunds:
    get: {summary: List refunds, tags: [Refunds]}
`
	writeImportFile(t, path, next)

	review, err := d.Specs.ReviewLinked(ctx, collection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Review.Added) != 1 || len(review.Review.Changed) != 1 || len(review.Review.Environments) != 1 {
		t.Fatalf("review:\n%s", describe(review.Review))
	}
	summary, err := d.Specs.Apply(ctx, review.Token, chosen(review.Review))
	if err != nil {
		t.Fatal(err)
	}
	if summary.Added != 1 || summary.Updated != 1 || summary.Removed != 0 || summary.EnvState == nil {
		t.Errorf("summary = %+v", summary)
	}

	list := requestNamed(t, d, collection.ID, "List all products")
	if list.URL != "{{baseUrl}}/products?page=5&sort=name" ||
		!slices.ContainsFunc(list.Headers, func(row domain.Row) bool { return row.Name == "X-Tenant" && row.Value == "acme" }) {
		t.Errorf("list = %q %+v, want both the person's work and the new parameter", list.URL, list.Headers)
	}
	requestNamed(t, d, collection.ID, "List refunds")

	workspace, err := d.Collections.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	held, err := d.Environments.Held(ctx, workspace)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, environment := range held {
		if environment.ID == staging.ID {
			for _, one := range environment.Block(collection.ID) {
				values[one.Name] = one.Value
			}
		}
	}
	if values["baseUrl"] != "https://staging.shop.test/v2" || values["apiToken"] != "s3cret" {
		t.Errorf("staging = %v, want the new address and the secret kept", values)
	}

	if _, err := d.Specs.Apply(ctx, review.Token, merge.Choices{}); domain.CodeOf(err) != domain.CodeImportExpired {
		t.Errorf("second Apply = %v, want %v", err, domain.CodeImportExpired)
	}
	again, err := d.Specs.ReviewLinked(ctx, collection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := describe(again.Review); got != "" {
		t.Errorf("after the update:\n%s", got)
	}

	if err := d.Collections.Delete(ctx, collection.ID); err != nil {
		t.Fatal(err)
	}
	if linked, err := d.Specs.Linked(ctx, collection.ID); err != nil || linked != nil {
		t.Errorf("Linked = %+v, %v, want the link gone with its collection", linked, err)
	}
}

// An update rewrites rows whole, so what the editor does not show must come through it.
func TestAnUpdateKeepsTheScriptsOfRequestsAndCollections(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "shop.yaml")
	writeImportFile(t, path, linkedSpec)
	d := newDevice(t)
	ctx := t.Context()
	collection := importSpec(t, d, path)
	list := requestNamed(t, d, collection.ID, "List products")
	for id, code := range map[string]string{collection.ID: "// collection", list.ID: "// request"} {
		if _, err := d.Scripts.SaveScripts(ctx, id, &domain.Scripts{Post: code}); err != nil {
			t.Fatal(err)
		}
	}

	next := strings.Replace(linkedSpec, `info: {title: Shop API, version: "1"}`,
		`info: {title: Shop API, version: "2", description: Everything the shop sells.}`, 1)
	next = strings.Replace(next, "summary: List products", "summary: List all products", 1)
	writeImportFile(t, path, next)
	review, err := d.Specs.ReviewLinked(ctx, collection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Specs.Apply(ctx, review.Token, chosen(review.Review)); err != nil {
		t.Fatal(err)
	}

	full, err := d.Collections.Full(ctx, collection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if full.Description != "Everything the shop sells." || full.Scripts == nil || full.Scripts.Post != "// collection" {
		t.Errorf("collection = %q %+v, want the new description beside its script", full.Description, full.Scripts)
	}
	if updated := requestNamed(t, d, collection.ID, "List all products"); updated.Scripts == nil ||
		updated.Scripts.Post != "// request" {
		t.Errorf("request scripts = %+v, want them kept", updated.Scripts)
	}
}

func TestAnExistingCollectionLinksByAddress(t *testing.T) {
	t.Parallel()
	served := linkedSpec
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(served))
	}))
	t.Cleanup(server.Close)

	d := newDevice(t)
	ctx := t.Context()
	// A pasted description is not linked: there is nowhere to read it again.
	preview, err := d.Transfer.PreviewText(ctx, "openapi", linkedSpec, "")
	if err != nil {
		t.Fatal(err)
	}
	summary, err := d.Transfer.Apply(ctx, preview.Token, "", "", "(imported)")
	if err != nil {
		t.Fatal(err)
	}
	collection := summary.Tree[0]
	if _, err := d.Specs.ReviewLinked(ctx, collection.ID); domain.CodeOf(err) != domain.CodeSpecNotLinked {
		t.Fatalf("ReviewLinked = %v, want %v", err, domain.CodeSpecNotLinked)
	}
	if kept, err := d.Store.SpecSnapshot(ctx, collection.ID); err != nil || len(kept) == 0 {
		t.Fatalf("snapshot of a pasted import = %d bytes, %v, want what it said", len(kept), err)
	}

	review, err := d.Specs.ReviewURL(ctx, collection.ID, " "+server.URL+" ")
	if err != nil {
		t.Fatal(err)
	}
	if got := describe(review.Review); got != "" {
		t.Fatalf("first review:\n%s", got)
	}
	if _, err := d.Specs.Apply(ctx, review.Token, chosen(review.Review)); err != nil {
		t.Fatal(err)
	}
	linked, err := d.Specs.Linked(ctx, collection.ID)
	if err != nil || linked == nil || linked.Kind != domain.SpecURL || linked.Location != server.URL || linked.ReadAt == 0 {
		t.Fatalf("Linked = %+v, %v, want the address", linked, err)
	}

	served = strings.Replace(linkedSpec, "summary: Create product", "summary: Add product", 1)
	review, err = d.Specs.ReviewLinked(ctx, collection.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The first update recorded the description, so the new name is no conflict.
	if changed := review.Review.Changed; len(changed) != 1 || changed[0].Changes[0].Conflict {
		t.Errorf("review:\n%s", describe(review.Review))
	}
}
