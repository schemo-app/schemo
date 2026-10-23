package insomnia

import (
	"bytes"
	"encoding/json"
	"testing"

	"schemo/internal/domain"
)

// exported is the bytes a collection and its environments write to, with the error checked: no
// scenario here is about a failure to write.
func exported(t *testing.T, collection domain.Collection,
	environments []domain.EnvironmentDraft, secrets bool) []byte {
	t.Helper()
	data, err := Export(collection, environments, secrets)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	return data
}

// roundTrip writes the export and reads it back: what every scenario here is about is that the
// reader keeps what the writer was handed.
func roundTrip(t *testing.T, collection domain.Collection,
	environments []domain.EnvironmentDraft, secrets bool) domain.Document {
	t.Helper()
	doc, err := Import(exported(t, collection, environments, secrets))
	if err != nil {
		t.Fatalf("Import of the export: %v", err)
	}
	return doc
}

// resourceRow is one resource of the written file, looked up by its kind and name in the JSON
// itself: these assertions are about what the file holds, not about what an import keeps.
func resourceRow(t *testing.T, data []byte, kind, name string) map[string]any {
	t.Helper()
	var file map[string]any
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("the export is not json: %v", err)
	}
	rows, _ := file["resources"].([]any)
	for _, one := range rows {
		object, ok := one.(map[string]any)
		if ok && object["_type"] == kind && object["name"] == name {
			return object
		}
	}
	t.Fatalf("the export holds no %s named %q", kind, name)
	return nil
}

// at walks a decoded resource by the keys and indexes a scenario names.
func at(t *testing.T, value any, path ...any) any {
	t.Helper()
	for _, step := range path {
		switch key := step.(type) {
		case string:
			object, ok := value.(map[string]any)
			if !ok {
				t.Fatalf("%v is not an object", value)
			}
			value = object[key]
		case int:
			list, ok := value.([]any)
			if !ok || key < 0 || key >= len(list) {
				t.Fatalf("%v holds no item %d", value, key)
			}
			value = list[key]
		}
	}
	return value
}

// storefront is the tree the scenarios walk: a request before a folder in the tree's own order,
// rows of both states, a template in the address and a sentence on each level.
func storefront() domain.Collection {
	return domain.Collection{
		Name:        "Storefront",
		Description: "The shop's own API.",
		Items: []domain.CollectionNode{{
			Name:        "List products",
			Position:    1,
			Method:      "get",
			URL:         "{{base}}/products?page[size]=25",
			Description: "Everything on the shelf.",
			Headers: []domain.Row{
				{Name: "Accept", Value: "application/vnd.api+json", Enabled: true},
				{Name: "X-Debug", Value: "1", Enabled: false},
			},
			Params: []domain.Row{
				{Name: "page[size]", Value: "25", Enabled: true},
				{Name: "include", Value: "offers", Enabled: false},
			},
		}},
		Children: []domain.Collection{{
			Name:        "Cart",
			Position:    2,
			Description: "Where the purchases go.",
			Items: []domain.CollectionNode{{
				Name:     "Add an item",
				Position: 1,
				Method:   "POST",
				URL:      "{{base}}/cart",
			}},
		}},
	}
}

// The tree travels as itself: the app's `{{base}}` is written Insomnia's way with the query moved
// into the parameter rows, and everything the reader keeps comes back as it was handed over.
func TestExportRoundTripsACollection(t *testing.T) {
	data := exported(t, storefront(), nil, false)
	written := resourceRow(t, data, "request", "List products")
	if url := at(t, written, "url"); url != "{{ _.base }}/products" {
		t.Errorf("url = %v, want Insomnia's own form of the template and no query on it", url)
	}
	cart := resourceRow(t, data, "request_group", "Cart")
	if cart["description"] != "Where the purchases go." {
		t.Errorf("description = %v, want the folder's own sentence in the file", cart["description"])
	}

	collection := only(t, roundTrip(t, storefront(), nil, false))
	if collection.Name != "Storefront" || collection.Description != "The shop's own API." {
		t.Errorf("collection = %+v, want the workspace's own name", collection)
	}
	level := collection.Level()
	if len(level) != 2 || level[0].Node == nil || level[0].Node.Name != "List products" {
		t.Fatalf("level = %+v, want the request before the Cart folder", level)
	}
	if level[1].Collection == nil || level[1].Collection.Name != "Cart" {
		t.Fatalf("level = %+v, want the Cart folder after it", level)
	}

	list := node(t, collection, "List products")
	if list.Method != "GET" || list.Description != "Everything on the shelf." {
		t.Errorf("list = %+v, want the method uppercased and the sentence back", list)
	}
	if list.URL != "{{base}}/products?page[size]=25" {
		t.Errorf("url = %q, want the template back and the enabled parameter in the address", list.URL)
	}
	if len(list.Headers) != 2 || !list.Headers[0].Enabled || list.Headers[0].Name != "Accept" {
		t.Errorf("headers = %+v, want the Accept header on", list.Headers)
	}
	if len(list.Headers) != 2 || list.Headers[1].Enabled {
		t.Errorf("headers = %+v, want the disabled one parked", list.Headers)
	}
	if len(list.Params) != 2 || !list.Params[0].Enabled || list.Params[0].Value != "25" {
		t.Errorf("params = %+v, want the enabled one first", list.Params)
	}
	if len(list.Params) != 2 || list.Params[1].Enabled || list.Params[1].Value != "offers" {
		t.Errorf("params = %+v, want the disabled one parked", list.Params)
	}

	added := node(t, collection, "Add an item")
	if added.Method != "POST" || added.URL != "{{base}}/cart" {
		t.Errorf("added = %+v, want the request inside the folder as it was", added)
	}
}

// The address is where this app keeps the query, so a query no row repeats still travels as
// parameter rows: the address loses it on the way out, and losing it would change the request.
func TestExportWritesAnAddressQueryAsParameterRows(t *testing.T) {
	collection := domain.Collection{
		Name: "Storefront",
		Items: []domain.CollectionNode{{
			Name: "Search", Position: 1, Method: "GET", URL: "{{base}}/items?page[size]=25",
		}},
	}
	written := resourceRow(t, exported(t, collection, nil, false), "request", "Search")
	if written["url"] != "{{ _.base }}/items" {
		t.Errorf("url = %v, want the query taken out of the address", written["url"])
	}
	rows, _ := written["parameters"].([]any)
	if len(rows) != 1 || at(t, rows[0], "name") != "page[size]" || at(t, rows[0], "value") != "25" {
		t.Errorf("parameters = %+v, want the address's own pair as a row", rows)
	}

	back := node(t, only(t, roundTrip(t, collection, nil, false)), "Search")
	if back.URL != "{{base}}/items?page[size]=25" || len(back.Params) != 1 || !back.Params[0].Enabled {
		t.Errorf("search = %+v, want the address back with its own query", back)
	}
}

// Every body kind this app sends round trips as itself: the media type says which it is, a form
// keeps its fields, and a file row comes back marked as a file with no value to send.
func TestExportRoundTripsBodiesByKind(t *testing.T) {
	collection := domain.Collection{
		Name: "Storefront",
		Items: []domain.CollectionNode{
			{Name: "Create", Position: 1, Method: "POST", URL: "{{base}}/products",
				BodyKind: domain.BodyJSON, Body: `{"name": "{{product}}"}`},
			{Name: "Upload", Position: 2, Method: "POST", URL: "{{base}}/files",
				BodyKind: domain.BodyXML, Body: "<name>{{product}}</name>"},
			{Name: "Attach", Position: 3, Method: "POST", URL: "{{base}}/receipt",
				BodyKind: domain.BodyForm, Form: []domain.FormRow{
					{Name: "note", Value: "paid", Enabled: true},
					{Name: "file", File: true, Src: "/Users/shop/receipt.png", Enabled: true},
				}},
			{Name: "Raw", Position: 4, Method: "POST", URL: "{{base}}/raw",
				BodyKind: domain.BodyRaw, Body: "hello {{who}}"},
		},
	}
	data := exported(t, collection, nil, false)
	body := resourceRow(t, data, "request", "Attach")["body"]
	fileRow, _ := at(t, body, "params", 1).(map[string]any)
	if fileRow["value"] != "" {
		t.Errorf("file row = %+v, want an empty value written beside the file", fileRow)
	}
	if fileRow["fileName"] != "/Users/shop/receipt.png" {
		t.Errorf("file row = %+v, want the path the file was chosen from", fileRow)
	}

	back := only(t, roundTrip(t, collection, nil, false))
	created := node(t, back, "Create")
	if created.BodyKind != domain.BodyJSON || created.Body != `{"name": "{{product}}"}` {
		t.Errorf("body = %+v, want the json text with its template back", created)
	}
	uploaded := node(t, back, "Upload")
	if uploaded.BodyKind != domain.BodyXML || uploaded.Body != "<name>{{product}}</name>" {
		t.Errorf("body = %+v, want the xml text with its template back", uploaded)
	}
	raw := node(t, back, "Raw")
	if raw.BodyKind != domain.BodyRaw || raw.Body != "hello {{who}}" {
		t.Errorf("body = %+v, want the raw text with its template back", raw)
	}
	attached := node(t, back, "Attach")
	if attached.BodyKind != domain.BodyForm || len(attached.Form) != 2 {
		t.Fatalf("form = %+v, want the two fields", attached.Form)
	}
	if !attached.Form[0].Enabled || attached.Form[0].Value != "paid" {
		t.Errorf("field = %+v, want the note as it was", attached.Form[0])
	}
	if !attached.Form[1].File || attached.Form[1].Value != "" || attached.Form[1].Name != "file" {
		t.Errorf("field = %+v, want the file marked with no value", attached.Form[1])
	}
}

// The schemes both tools share travel as themselves: a bearer with a prefix of its own, a basic
// pair, an api key and its place, `none` where it was asked for, and nothing for inherit.
func TestExportRoundTripsAuthorization(t *testing.T) {
	collection := domain.Collection{
		Name: "Storefront",
		Items: []domain.CollectionNode{
			{Name: "Bearer", Position: 1, Method: "GET", URL: "{{base}}/a",
				Auth: &domain.Auth{Type: domain.AuthBearer, Fields: map[string]string{
					"prefix": "Token", "token": "{{api_key}}"}}},
			{Name: "Basic", Position: 2, Method: "GET", URL: "{{base}}/b",
				Auth: &domain.Auth{Type: domain.AuthBasic, Fields: map[string]string{
					"username": "shop", "password": "{{password}}"}}},
			{Name: "Key", Position: 3, Method: "GET", URL: "{{base}}/c",
				Auth: &domain.Auth{Type: domain.AuthAPIKey, Fields: map[string]string{
					"key": "api_key", "value": "{{api_key}}", "place": domain.PlaceQuery}}},
			{Name: "None", Position: 4, Method: "GET", URL: "{{base}}/d",
				Auth: &domain.Auth{Type: domain.AuthNone}},
			{Name: "Inherit", Position: 5, Method: "GET", URL: "{{base}}/e"},
		},
	}
	back := only(t, roundTrip(t, collection, nil, false))

	bearer := node(t, back, "Bearer").Auth
	if bearer == nil || bearer.Type != domain.AuthBearer ||
		bearer.Answer("prefix") != "Token" || bearer.Answer("token") != "{{api_key}}" {
		t.Errorf("auth = %+v, want the bearer scheme with its own prefix", bearer)
	}
	basic := node(t, back, "Basic").Auth
	if basic == nil || basic.Type != domain.AuthBasic ||
		basic.Answer("username") != "shop" || basic.Answer("password") != "{{password}}" {
		t.Errorf("auth = %+v, want the basic pair", basic)
	}
	key := node(t, back, "Key").Auth
	if key == nil || key.Type != domain.AuthAPIKey || key.Answer("place") != domain.PlaceQuery ||
		key.Answer("key") != "api_key" || key.Answer("value") != "{{api_key}}" {
		t.Errorf("auth = %+v, want the api key sent as a query parameter", key)
	}
	if none := node(t, back, "None").Auth; none == nil || none.Type != domain.AuthNone {
		t.Errorf("auth = %+v, want none where the app chose it", none)
	}
	if inherit := node(t, back, "Inherit").Auth; inherit != nil {
		t.Errorf("auth = %+v, want nothing at all where the app inherits", inherit)
	}
}

// A secret's value leaves the app only when the caller says the export is for somebody who may see
// it; its name still travels, so the other side knows there is something to fill in.
func TestExportHoldsBackSecretsUntilAsked(t *testing.T) {
	environments := []domain.EnvironmentDraft{{
		Name: "Dev",
		Variables: []domain.Variable{
			{Name: "base_url", Value: "https://api.dev.example", Kind: domain.VariableText, Enabled: true},
			{Name: "dev_token", Value: "tok_dev", Kind: domain.VariableSecret, Enabled: true},
		},
	}}

	closed := resourceRow(t, exported(t, storefront(), environments, false), "environment", "Dev")
	values, _ := closed["data"].(map[string]any)
	if values["base_url"] != "https://api.dev.example" {
		t.Errorf("data = %+v, want the plain value carried", values)
	}
	if value, held := values["dev_token"]; !held || value != "" {
		t.Errorf("data = %+v, want the secret's name with nothing beside it", values)
	}

	open := resourceRow(t, exported(t, storefront(), environments, true), "environment", "Dev")
	if value := at(t, open, "data", "dev_token"); value != "tok_dev" {
		t.Errorf("data = %+v, want the value once it was asked for", open["data"])
	}

	back := roundTrip(t, storefront(), environments, true)
	if len(back.Environments) != 1 || back.Environments[0].Name != "Dev" {
		t.Fatalf("environments = %+v, want Dev back", back.Environments)
	}
	url := variable(t, back.Environments[0].Variables, "base_url")
	if url.Value != "https://api.dev.example" || url.Kind != domain.VariableText {
		t.Errorf("base_url = %+v, want the plain value back", url)
	}
	token := variable(t, back.Environments[0].Variables, "dev_token")
	if token.Value != "tok_dev" || token.Kind != domain.VariableSecret {
		t.Errorf("dev_token = %+v, want the value and the name's own kind", token)
	}
}

// Ids are counted rather than made up and the walk is the tree's own, so the same collection writes
// the same bytes every time — an export two runs cannot be diffed is one nobody can check.
func TestExportWritesTheSameBytesEveryTime(t *testing.T) {
	environments := []domain.EnvironmentDraft{{Name: "Dev", Variables: []domain.Variable{
		{Name: "base_url", Value: "https://api.dev.example", Kind: domain.VariableText, Enabled: true},
		{Name: "dev_token", Value: "tok_dev", Kind: domain.VariableSecret, Enabled: true},
		{Name: "region", Value: "eu", Kind: domain.VariableText, Enabled: true},
	}}}
	first := exported(t, storefront(), environments, true)
	second := exported(t, storefront(), environments, true)
	if !bytes.Equal(first, second) {
		t.Errorf("two exports of one collection differ:\n%s\n%s", first, second)
	}
	if id := at(t, resourceRow(t, first, "workspace", "Storefront"), "_id"); id != "wrk_1" {
		t.Errorf("workspace id = %v, want the counted one", id)
	}
}

// A path parameter's value is written beside the url, where Insomnia keeps it, and read back into
// the row it came from.
func TestExportRoundTripsPathParameters(t *testing.T) {
	collection := domain.Collection{
		Name: "Storefront",
		Items: []domain.CollectionNode{{
			Name: "One item", Position: 1, Method: "GET", URL: "{{base}}/items/:item",
			PathParams: []domain.Row{
				{Name: "item", Value: "it_1", Enabled: true, Description: "Which item"},
			},
		}},
	}
	written := resourceRow(t, exported(t, collection, nil, false), "request", "One item")
	if written["url"] != "{{ _.base }}/items/:item" {
		t.Errorf("url = %v, want the path parameter left in the address", written["url"])
	}
	rows, _ := written["pathParameters"].([]any)
	if len(rows) != 1 || at(t, rows[0], "name") != "item" || at(t, rows[0], "value") != "it_1" {
		t.Errorf("pathParameters = %+v, want the row's value beside the url", rows)
	}

	back := node(t, only(t, roundTrip(t, collection, nil, false)), "One item")
	if len(back.PathParams) != 1 || back.PathParams[0].Value != "it_1" ||
		back.PathParams[0].Description != "Which item" {
		t.Errorf("path params = %+v, want the row back", back.PathParams)
	}
}
