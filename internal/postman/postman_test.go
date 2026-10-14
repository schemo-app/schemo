package postman

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"

	"schemo/internal/domain"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	return data
}

// imported reads bytes as the one collection they hold. What the document around a collection is
// for is exercised where the formats meet; this file is about the format itself.
func imported(t *testing.T, data []byte) (domain.Collection, error) {
	t.Helper()
	doc, err := Read(data)
	if err != nil {
		return domain.Collection{}, err
	}
	if len(doc.Collections) != 1 {
		t.Fatalf("read %d collections, want one", len(doc.Collections))
	}
	return doc.Collections[0], nil
}

// asRequest is the fixture row that must be a request, with the index checked here.
func asRequest(t *testing.T, collection domain.Collection, i int) domain.CollectionNode {
	t.Helper()
	if i >= len(collection.Items) {
		t.Fatalf("the level holds %d requests, want one at %d", len(collection.Items), i)
	}
	return collection.Items[i]
}

func TestImportReadsNestedCollectionsAndRequests(t *testing.T) {
	collection, err := imported(t, fixture(t, "collection.json"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if collection.Name != "Storefront" {
		t.Errorf("name = %q, want the file's", collection.Name)
	}

	// The file writes one list; the app keeps two, because a folder is a collection inside one and a
	// request is a row of the level it is in.
	if len(collection.Children) != 1 || len(collection.Items) != 1 {
		t.Fatalf("level = %d children and %d requests, want a folder and a request",
			len(collection.Children), len(collection.Items))
	}
	nested := collection.Children[0]
	if nested.Name != "Пользователи" || len(nested.Items) != 2 {
		t.Fatalf("nested = %+v, want the folder with its two requests", nested)
	}
	// The two share one number line, numbered by where each row stood in the file, so the level merges
	// back into the file's order rather than into two groups.
	if nested.Position != 0 || collection.Items[0].Position != 1 {
		t.Errorf("positions = %d and %d, want the order the file is written in",
			nested.Position, collection.Items[0].Position)
	}
	if order := collection.Level(); order[0].Collection == nil || order[1].Node == nil {
		t.Errorf("level = %+v, want the folder first and the request after it", order)
	}
	if nested.Items[1].Position != 1 {
		t.Errorf("position inside the folder = %d, want the order the file is written in",
			nested.Items[1].Position)
	}

	list := asRequest(t, nested, 0)
	if list.Method != "GET" ||
		list.URL != "https://api.example.com/users?include=author,comments&page[size]=25" {
		t.Errorf("request = %+v, want the method and the address", list)
	}
	// A row the file switched off stays switched off: it is a thing about the request.
	if len(list.Headers) != 2 || list.Headers[1].Name != "X-Debug" || list.Headers[1].Enabled {
		t.Errorf("headers = %+v, want the disabled one kept and disabled", list.Headers)
	}
	// A parameter that is not in the address is still the request's: it travels in the list beside
	// it, which is where Postman keeps it too.
	if len(list.Params) != 3 || list.Params[2].Name != "filter[state]" || list.Params[2].Enabled {
		t.Errorf("params = %+v, want the disabled one kept", list.Params)
	}
	if list.Auth == nil || list.Auth.Type != domain.AuthBearer ||
		list.Auth.Answer("token") != "{{token}}" {
		t.Errorf("auth = %+v, want the bearer token as it was written", list.Auth)
	}

	create := asRequest(t, nested, 1)
	if create.Body != "{\n  \"data\": {\n    \"type\": \"users\"\n  }\n}" {
		t.Errorf("body = %q, want it as it was written", create.Body)
	}
	if len(create.Headers) != 1 || create.Headers[0].Name != "Content-Type" {
		t.Errorf("headers = %+v", create.Headers)
	}
	// An address that is a string and not an object is the same address.
	if create.URL != "https://api.example.com/users" {
		t.Errorf("url = %q", create.URL)
	}
}

// A form written as rows becomes the form it means; a body of a kind the app cannot send is left
// out rather than half-read.
func TestImportReadsAFormBody(t *testing.T) {
	collection, err := imported(t, fixture(t, "collection.json"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	report := asRequest(t, collection, 0)
	if report.Body != "from=2026-01-01" {
		t.Errorf("body = %q, want the enabled rows of the form", report.Body)
	}
	// Basic carries two values, and both of them are the request's: a login is not a secret and a
	// password is, and they have to come back the way they were written.
	if report.Auth == nil || report.Auth.Type != domain.AuthBasic ||
		report.Auth.Answer("username") != "reader" ||
		report.Auth.Answer("password") != "{{secret}}" {
		t.Errorf("auth = %+v, want both halves of the pair", report.Auth)
	}
}

// A folder's authorization is what everything inside it inherits, and the format has a place for
// it: a group that lost its auth on the way in would be a group that behaved differently here.
func TestImportReadsTheAuthOfANestedCollection(t *testing.T) {
	file := []byte(`{"info":{"name":"Магазин","schema":"` + Schema + `"},"item":[
		{"name":"Админ","auth":{"type":"bearer","bearer":[{"key":"token","value":"{{admin}}"}]},
		 "item":[{"name":"Список",
		          "request":{"method":"GET","url":{"raw":"https://api.example.com/admins"}}}]}]}`)

	collection, err := imported(t, file)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(collection.Children) != 1 {
		t.Fatalf("children = %+v, want the group", collection.Children)
	}
	nested := collection.Children[0]
	if nested.Auth == nil || nested.Auth.Answer("token") != "{{admin}}" {
		t.Errorf("auth = %+v, want the group's own", nested.Auth)
	}

	// And back out again, on the group rather than on the requests inside it.
	data, err := Export(collection)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	var doc collectionFile
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the export does not read back: %v", err)
	}
	if len(doc.Item) != 1 || doc.Item[0].Auth == nil || doc.Item[0].Request != nil {
		t.Fatalf("item = %+v, want the group with its auth", doc.Item)
	}
	if doc.Item[0].Auth.Extra["bearer"][0].Value != "{{admin}}" {
		t.Errorf("auth = %+v, want the token as it was written", doc.Item[0].Auth)
	}
}

// A request the file says sends nothing must not come back as one that inherits: the collection's
// credentials would go out where the file said none.
func TestNoauthStaysNoauthOnTheWayBack(t *testing.T) {
	file := []byte(`{"info":{"name":"Магазин","schema":"` + Schema + `"},
		"auth":{"type":"bearer","bearer":[{"key":"token","value":"{{token}}"}]},
		"item":[{"name":"Статус","request":{"method":"GET",
			"url":{"raw":"https://api.example.com/status"},
			"auth":{"type":"noauth"}}}]}`)

	collection, err := imported(t, file)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	status := collection.Items[0]
	if status.Auth == nil || status.Auth.Type != domain.AuthNone {
		t.Errorf("auth = %+v, want none where the file said noauth", status.Auth)
	}

	// And the word is written back: a request that lost it would inherit on the next read.
	data, err := Export(collection)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	var doc collectionFile
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the export does not read back: %v", err)
	}
	if len(doc.Item) != 1 || doc.Item[0].Request == nil || doc.Item[0].Request.Auth == nil ||
		doc.Item[0].Request.Auth.Type != "noauth" {
		t.Fatalf("item = %+v, want the request with its noauth", doc.Item)
	}
}

// An OAuth 2.0 scheme comes in whole — the addresses, the client pair, the password grant's own
// two answers — and goes back out under Postman's names for them. The grant the file never names is
// the flow Postman's dialog runs.
func TestOAuth2StaysOAuth2OnTheWayBack(t *testing.T) {
	file := []byte(`{"info":{"name":"Магазин","schema":"` + Schema + `"},"item":[
		{"name":"Токен","request":{"method":"GET","url":{"raw":"https://api.example.com/token"},
			"auth":{"type":"oauth2","oauth2":[
				{"key":"authUrl","value":"https://example.com/auth"},
				{"key":"accessTokenUrl","value":"https://example.com/token"},
				{"key":"clientId","value":"{{clientId}}"},
				{"key":"username","value":"owner"},
				{"key":"password","value":"{{ownerPassword}}"},
				{"key":"scope","value":"read write"}]}}}]}`)

	collection, err := imported(t, file)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	auth := collection.Items[0].Auth
	if auth == nil || auth.Type != domain.AuthOAuth2 {
		t.Fatalf("auth = %+v, want the oauth2 scheme", auth)
	}
	if auth.Answer("grant") != "authorization_code" ||
		auth.Answer("authUrl") != "https://example.com/auth" ||
		auth.Answer("tokenUrl") != "https://example.com/token" ||
		auth.Answer("owner") != "owner" || auth.Answer("ownerPassword") != "{{ownerPassword}}" ||
		auth.Answer("scope") != "read write" {
		t.Errorf("auth = %+v, want every field the file carried", auth)
	}

	// And back out again, under Postman's own names.
	data, err := Export(collection)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	var doc collectionFile
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the export does not read back: %v", err)
	}
	written := doc.Item[0].Request.Auth
	if written == nil || written.Type != "oauth2" {
		t.Fatalf("auth = %+v, want the oauth2 scheme written", written)
	}
	rows := written.Extra["oauth2"]
	if fieldValue(rows, "authUrl") != "https://example.com/auth" ||
		fieldValue(rows, "accessTokenUrl") != "https://example.com/token" ||
		fieldValue(rows, "username") != "owner" ||
		fieldValue(rows, "password") != "{{ownerPassword}}" {
		t.Errorf("auth = %+v, want the fields under Postman's names", rows)
	}
}

// A saved answer travels both ways — the name, the code, the headers and the body — and the NUL
// bytes Postman pads bodies with are left behind: a NUL is not text this app can carry.
func TestResponseExamplesTravelBothWays(t *testing.T) {
	file := []byte(`{"info":{"name":"Магазин","schema":"` + Schema + `"},"item":[
		{"name":"Товары","request":{"method":"GET","url":{"raw":"https://api.example.com/products"}},
		 "response":[{"name":"Список","code":200,
			"header":[{"key":"Content-Type","value":"application/json"}],
			"body":"{\"data\":[]}\u0000"}]}]}`)

	collection, err := imported(t, file)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	example := collection.Items[0].Responses[0]
	if example.Name != "Список" || example.Status != 200 || example.Body != `{"data":[]}` {
		t.Fatalf("example = %+v, want the saved answer without its NUL bytes", example)
	}
	if len(example.Headers) != 1 || example.Headers[0].Name != "Content-Type" {
		t.Errorf("headers = %+v, want the answer's own", example.Headers)
	}

	// And back out as the file keeps them.
	data, err := Export(collection)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	var doc collectionFile
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the export does not read back: %v", err)
	}
	if len(doc.Item) != 1 || len(doc.Item[0].Response) != 1 {
		t.Fatalf("item = %+v, want the example written", doc.Item)
	}
	saved := doc.Item[0].Response[0]
	if saved.Name != "Список" || saved.Code != 200 || saved.Body != `{"data":[]}` ||
		len(saved.Header) != 1 {
		t.Errorf("saved = %+v, want what came in", saved)
	}
}

func TestImportRefusesAFileThatIsNotACollection(t *testing.T) {
	for name, data := range map[string]string{
		"чужой JSON":    `{"openapi": "3.0.0"}`,
		"просто массив": `[1, 2, 3]`,
		"не JSON":       `curl -X GET https://example.com`,
	} {
		if _, err := imported(t, []byte(data)); !errors.Is(err, domain.ErrNotAllowed) {
			t.Errorf("%s: Import = %v, want ErrNotAllowed", name, err)
		}
	}
}

// The round trip is what makes an export worth anything: a collection written out and read back is
// the collection it was, the collections inside it included.
func TestRoundTrip(t *testing.T) {
	first, err := imported(t, fixture(t, "collection.json"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	data, err := Export(first)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	second, err := imported(t, data)
	if err != nil {
		t.Fatalf("Import after Export: %v", err)
	}

	compareCollection(t, first, second)
	if !json.Valid(data) {
		t.Error("the export is not valid JSON")
	}
}

// A row's description travels both ways: Postman writes it as a string or as a `content` object,
// both are read, and what was read is written back as the row came back.
func TestRowDescriptionsTravelBothWays(t *testing.T) {
	document := []byte(`{
		"info": {"name": "Storefront",
			"schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"item": [{
			"name": "List products",
			"request": {
				"method": "GET",
				"url": {"raw": "https://api.example.com/products?page=2",
					"query": [{"key": "page", "value": "2", "description": "Which page"}]},
				"header": [{"key": "Accept", "value": "application/json",
					"description": {"content": "The media type"}}]
			}
		}]
	}`)

	first, err := imported(t, document)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	assertRowDescriptions(t, first)

	data, err := Export(first)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	back, err := imported(t, data)
	if err != nil {
		t.Fatalf("Import after Export: %v", err)
	}
	assertRowDescriptions(t, back)
}

// A path parameter is the request's own: Postman keeps its value beside the address, and it goes
// into the row that fills the `:name` and comes back out of it.
func TestPathParametersTravelBothWays(t *testing.T) {
	document := []byte(`{
		"info": {"name": "Storefront",
			"schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"item": [{
			"name": "One product",
			"request": {
				"method": "GET",
				"url": {"raw": "{{baseUrl}}/products/:product/prices/:price",
					"variable": [
						{"key": "product", "value": "42", "description": "Which product"},
						{"key": "stale", "value": "x"}
					]}
			}
		}]
	}`)

	check := func(collection domain.Collection) {
		t.Helper()
		node := asRequest(t, collection, 0)
		want := []domain.Row{
			{Name: "product", Value: "42", Enabled: true, Description: "Which product"},
			{Name: "price", Enabled: true},
		}
		if !slices.Equal(node.PathParams, want) {
			t.Errorf("path params = %+v, want %+v", node.PathParams, want)
		}
		if len(collection.Variables) != 0 {
			t.Errorf("variables = %+v, want a path parameter that is no variable", collection.Variables)
		}
	}

	first, err := imported(t, document)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	check(first)

	data, err := Export(first)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	back, err := imported(t, data)
	if err != nil {
		t.Fatalf("Import after Export: %v", err)
	}
	check(back)
}

func assertRowDescriptions(t *testing.T, collection domain.Collection) {
	t.Helper()
	if len(collection.Items) != 1 {
		t.Fatalf("items = %d, want one", len(collection.Items))
	}
	node := collection.Items[0]
	if len(node.Params) != 1 || node.Params[0].Description != "Which page" {
		t.Errorf("params = %+v, want the string form of the description read", node.Params)
	}
	if len(node.Headers) != 1 || node.Headers[0].Description != "The media type" {
		t.Errorf("headers = %+v, want the object form of the description read", node.Headers)
	}
}

func compareCollection(t *testing.T, want domain.Collection, got domain.Collection) {
	t.Helper()
	if got.Name != want.Name || got.Position != want.Position {
		t.Errorf("collection = %+v, want %+v", got, want)
	}
	if len(got.Items) != len(want.Items) {
		t.Fatalf("%q holds %d requests, want %d", got.Name, len(got.Items), len(want.Items))
	}
	for i := range want.Items {
		compareNode(t, want.Items[i], got.Items[i])
	}
	if len(got.Children) != len(want.Children) {
		t.Fatalf("%q holds %d collections, want %d", got.Name, len(got.Children), len(want.Children))
	}
	for i := range want.Children {
		compareCollection(t, want.Children[i], got.Children[i])
	}
	// What the level answers for `{{tokens}}`, in the order it answers them: the nearest answer is the
	// last one, so the order is part of the answer.
	if len(got.Variables) != len(want.Variables) {
		t.Fatalf("%q answers %d names, want %d", want.Name, len(got.Variables), len(want.Variables))
	}
	for i := range want.Variables {
		if got.Variables[i] != want.Variables[i] {
			t.Errorf("%q variable %d = %+v, want %+v", want.Name, i, got.Variables[i], want.Variables[i])
		}
	}
}

// What a level answers for `{{tokens}}` travels with it both ways. A collection handed to somebody
// else has to mean the same thing on the other side, which is why a level owns its own.
func TestCollectionVariablesTravelBothWays(t *testing.T) {
	original := domain.Collection{
		Name: "Магазин",
		Variables: []domain.Variable{
			{Name: "baseUrl", Value: "https://api.example.com", Kind: domain.VariableText, Enabled: true},
			{Name: "page", Value: "2", Kind: domain.VariableText, Enabled: false},
		},
		Children: []domain.Collection{{
			Name: "Пользователи",
			Variables: []domain.Variable{
				{Name: "version", Value: "v2", Kind: domain.VariableText, Enabled: true},
			},
		}},
	}

	data, err := Export(original)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	back, err := imported(t, data)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if len(back.Variables) != 2 {
		t.Fatalf("level = %+v, want the collection's two", back.Variables)
	}
	if back.Variables[0].Name != "baseUrl" || back.Variables[0].Value != "https://api.example.com" {
		t.Errorf("first = %+v, want the value that was written", back.Variables[0])
	}
	// A row the file marks disabled comes back disabled: it is a thing about the variable, and not a
	// comment the file left in passing.
	if back.Variables[1].Enabled {
		t.Errorf("second = %+v, want the disabled row kept disabled", back.Variables[1])
	}

	if len(back.Children) != 1 || len(back.Children[0].Variables) != 1 {
		t.Fatalf("nested = %+v, want the folder's own answer", back.Children)
	}
	if v := back.Children[0].Variables[0]; v.Name != "version" || v.Value != "v2" {
		t.Errorf("nested variable = %+v, want the one the folder answers", v)
	}
}

// A file may write a value as a number or a flag: the format allows any JSON, and what a request
// substitutes is text, so the JSON spelling of it is what the file meant.
func TestAVariableWrittenWithoutQuotesImportsAsItsSpelling(t *testing.T) {
	collection, err := imported(t, []byte(`{
		"info": {"name": "Numbers"},
		"variable": [{"key": "page", "value": 2}, {"key": "all", "value": true}],
		"item": []
	}`))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	got := map[string]string{}
	for _, v := range collection.Variables {
		got[v.Name] = v.Value
	}
	if got["page"] != "2" || got["all"] != "true" {
		t.Errorf("variables = %+v, want the spelling of each value", collection.Variables)
	}
}

func compareNode(t *testing.T, want domain.CollectionNode, got domain.CollectionNode) {
	t.Helper()
	if got.Name != want.Name || got.Position != want.Position {
		t.Errorf("node = %+v, want %+v", got, want)
	}
	if got.Method != want.Method || got.URL != want.URL || got.Body != want.Body {
		t.Errorf("%q = %+v, want %+v", want.Name, got, want)
	}
	if len(got.Headers) != len(want.Headers) || len(got.Params) != len(want.Params) {
		t.Errorf("%q kept %d headers and %d params, want %d and %d",
			want.Name, len(got.Headers), len(got.Params), len(want.Headers), len(want.Params))
	}
	if (got.Auth == nil) != (want.Auth == nil) {
		t.Errorf("%q auth = %+v, want %+v", want.Name, got.Auth, want.Auth)
	} else if got.Auth != nil && !sameAuth(*got.Auth, *want.Auth) {
		t.Errorf("%q auth = %+v, want %+v", want.Name, *got.Auth, *want.Auth)
	}
}

// sameAuth compares two authorizations the way a person would: the same scheme and the same answers
// to its fields, whether or not the map happened to be built in the same order.
func sameAuth(got domain.Auth, want domain.Auth) bool {
	if got.Type != want.Type || len(got.Fields) != len(want.Fields) {
		return false
	}
	for key, value := range want.Fields {
		if got.Answer(key) != value {
			return false
		}
	}
	return true
}

// An export of one request is a file with one item — which is what the context menu promises.
func TestExportWritesASingleRequest(t *testing.T) {
	collection, err := imported(t, fixture(t, "collection.json"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	single := asRequest(t, collection, 0)

	data, err := Export(domain.Collection{Name: single.Name, Items: []domain.CollectionNode{single}})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	var doc collectionFile
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the export does not read back: %v", err)
	}
	if doc.Info.Name != "Отчёт" || doc.Info.Schema != Schema {
		t.Errorf("info = %+v", doc.Info)
	}
	if len(doc.Item) != 1 || doc.Item[0].Request == nil {
		t.Fatalf("items = %+v, want the one request", doc.Item)
	}
	if doc.Item[0].Request.Method != "POST" {
		t.Errorf("method = %q", doc.Item[0].Request.Method)
	}
}

// A form body survives the trip: Postman has a shape for rows, and the app has one too.
func TestAFormBodyTravelsBothWays(t *testing.T) {
	file := []byte(`{"info":{"name":"Загрузка","schema":"` + Schema + `"},"item":[
		{"name":"Создать товар",
		 "request":{"method":"POST","url":{"raw":"https://api.example.com/products"},
		 "body":{"mode":"formdata","formdata":[
			{"key":"title","value":"Кофемолка Orion"},
			{"key":"off","value":"нет","disabled":true},
			{"key":"photo","src":"/Users/me/logo.png","type":"file"}]}}}]}`)

	collection, err := imported(t, file)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	node := asRequest(t, collection, 0)

	if node.BodyKind != domain.BodyForm {
		t.Errorf("bodyKind = %q, want a form", node.BodyKind)
	}
	if node.Body != "" {
		t.Errorf("body = %q, want the text left empty for a form", node.Body)
	}
	if len(node.Form) != 3 {
		t.Fatalf("form = %+v, want all three rows", node.Form)
	}
	if node.Form[0].Name != "title" || node.Form[0].Value != "Кофемолка Orion" ||
		!node.Form[0].Enabled {
		t.Errorf("form[0] = %+v", node.Form[0])
	}
	if node.Form[1].Enabled {
		t.Error("a row the file marks disabled came back switched on")
	}
	// The paperclip is read from the presence of a path, which is exactly how Postman says "file".
	if !node.Form[2].File || node.Form[2].Src != "/Users/me/logo.png" {
		t.Errorf("form[2] = %+v, want the file row and its path", node.Form[2])
	}

	data, err := Export(collection)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	var doc collectionFile
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the export does not read back: %v", err)
	}
	out := doc.Item[0].Request.Body
	if out == nil || out.Mode != "formdata" {
		t.Fatalf("body = %+v, want a formdata one", out)
	}
	if len(out.FormData) != 3 {
		t.Fatalf("formdata = %+v, want all three rows", out.FormData)
	}
	if out.FormData[1].Disabled != true {
		t.Errorf("formdata[1] = %+v, want the switch kept", out.FormData[1])
	}
	if out.FormData[2].Src != "/Users/me/logo.png" {
		t.Errorf("formdata[2] = %+v, want the path kept", out.FormData[2])
	}
}

// A binary body is a path, and it is the only one the app can name: the bytes never enter the file.
func TestABinaryBodyTravelsBothWays(t *testing.T) {
	file := []byte(`{"info":{"name":"Файлы","schema":"` + Schema + `"},"item":[
		{"name":"Загрузить","request":{"method":"POST","url":{"raw":"https://api.example.com/files"},
		 "body":{"mode":"file","file":{"src":"/tmp/report.pdf"}}}}]}`)

	collection, err := imported(t, file)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	node := asRequest(t, collection, 0)
	if node.BodyKind != domain.BodyBinary || node.BodyFile != "/tmp/report.pdf" {
		t.Errorf("node = %+v, want a binary body and its path", node)
	}

	data, err := Export(collection)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	var doc collectionFile
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the export does not read back: %v", err)
	}
	out := doc.Item[0].Request.Body
	if out == nil || out.Mode != "file" || out.File == nil || out.File.Src != "/tmp/report.pdf" {
		t.Errorf("body = %+v, want the path written back", out)
	}
}

// The three text kinds leave as raw, because Postman has nowhere to record which of them a body
// was. Writing a language this reader does not honour would be a promise the next import breaks.
func TestAJsonBodyLeavesAsRaw(t *testing.T) {
	node := domain.CollectionNode{
		Name: "Создать", Method: "POST",
		URL: "https://api.example.com/products", Body: `{"a": 1}`, BodyKind: domain.BodyJSON,
	}
	body := exportedBody(node)
	if body == nil || body.Mode != "raw" || body.Raw != `{"a": 1}` {
		t.Errorf("body = %+v, want raw text", body)
	}

	// A request with nothing in it has no body at all, not an empty one.
	empty := domain.CollectionNode{BodyKind: domain.BodyJSON}
	if body := exportedBody(empty); body != nil {
		t.Errorf("body = %+v, want none for a request with nothing in it", body)
	}
	// A file body with no file picked is the same kind of nothing.
	unpicked := domain.CollectionNode{BodyKind: domain.BodyBinary}
	if body := exportedBody(unpicked); body != nil {
		t.Errorf("body = %+v, want none for a binary body with no file", body)
	}
}

// A file's events are the code a level or a request runs: "prerequest" before it is sent and "test"
// after the answer, with the off switch the file wrote kept as it is.
func TestImportReadsScriptsFromEvents(t *testing.T) {
	doc, err := Read(fixture(t, "collection_events.json"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	collection := doc.Collections[0]
	if collection.Scripts == nil || collection.Scripts.Pre != "console.log('collection')" {
		t.Fatalf("collection scripts = %+v, want the pre-request event", collection.Scripts)
	}

	folder := collection.Children[0]
	if folder.Scripts == nil || folder.Scripts.Post != "pm.test('folder', () => {});" {
		t.Fatalf("folder scripts = %+v, want the event written as one string", folder.Scripts)
	}

	node := folder.Items[0]
	if node.Scripts == nil {
		t.Fatalf("node scripts = %+v, want the two events", node.Scripts)
	}
	if node.Scripts.Pre != "vars.set('id', '1')\nconsole.log('pre')" {
		t.Errorf("pre = %q, want the lines joined", node.Scripts.Pre)
	}
	if !node.Scripts.PostOff {
		t.Errorf("post = %+v, want the disabled event switched off rather than dropped", node.Scripts)
	}
}

// What a file says about a collection, a folder or a request arrives whole: Postman writes an
// object with a content, files written by hand write the sentence itself.
func TestImportReadsDescriptionsWrittenBothWays(t *testing.T) {
	doc, err := Read(fixture(t, "collection_events.json"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	collection := doc.Collections[0]
	if collection.Description != "Money moves here." {
		t.Errorf("collection description = %q, want the object's content", collection.Description)
	}
	if folder := collection.Children[0]; folder.Description != "A folder as Postman writes it." {
		t.Errorf("folder description = %q, want the string", folder.Description)
	}
	if node := collection.Children[0].Items[0]; node.Description != "One charge." {
		t.Errorf("request description = %q, want the string", node.Description)
	}
}

// A collection variable here cannot be secret — a collection leaves the app when it is exported —
// so a value the file marks secret arrives as its name with nothing in it, and the count of what
// was left behind travels with the document.
func TestImportLeavesASecretCollectionValueEmpty(t *testing.T) {
	doc, err := Read(fixture(t, "collection_events.json"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var found bool
	for _, variable := range doc.Collections[0].Variables {
		if variable.Name != "token" {
			continue
		}
		found = true
		if variable.Value != "" {
			t.Errorf("token = %q, want the value left behind", variable.Value)
		}
	}
	if !found {
		t.Fatalf("variables = %+v, want the secret's name kept", doc.Collections[0].Variables)
	}
	if len(doc.Skipped) != 1 || doc.Skipped[0].Code != domain.CodeSkippedSecretValue ||
		doc.Skipped[0].Count != 1 {
		t.Errorf("skipped = %+v, want one secret value counted", doc.Skipped)
	}
}

// An environment file is the other thing Postman exports: it holds no requests and its values are
// where secrets belong here, so they arrive as secrets.
func TestImportReadsAPostmanEnvironment(t *testing.T) {
	doc, err := Read(fixture(t, "environment.json"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(doc.Collections) != 0 || len(doc.Environments) != 1 {
		t.Fatalf("document = %+v, want one environment and no collections", doc)
	}
	draft := doc.Environments[0]
	if draft.Name != "Staging" || len(draft.Variables) != 3 {
		t.Fatalf("draft = %+v, want the named environment with its three values", draft)
	}
	if draft.Variables[1].Kind != domain.VariableSecret || draft.Variables[1].Value != "st-42" {
		t.Errorf("token = %+v, want a secret carrying its value", draft.Variables[1])
	}
	if draft.Variables[2].Enabled {
		t.Errorf("debug = %+v, want the disabled value disabled", draft.Variables[2])
	}
}

// The globals scope is a different thing from an environment, and reading it as one would be
// inventing a name for a file that never had one.
func TestImportRefusesTheGlobalsScope(t *testing.T) {
	globals := []byte(`{"_postman_variable_scope": "globals", "values": [{"key": "a", "value": "1"}]}`)
	if _, err := Read(globals); !errors.Is(err, domain.ErrNotAllowed) {
		t.Errorf("Read = %v, want the globals scope refused", err)
	}
}

// A round trip keeps the code and the sentences: an export that dropped them would be a file that
// means less than the collection it came from.
func TestExportWritesScriptsAndDescriptions(t *testing.T) {
	collection := domain.Collection{
		Name:        "Payments",
		Description: "Money moves here.",
		Scripts:     &domain.Scripts{Pre: "vars.set('a', '1')", Post: "pm.test('ok', () => {});"},
		Items: []domain.CollectionNode{{
			ID: "n1", CollectionID: "c1", Name: "Charge", Position: 0,
			Method: "POST", URL: "https://api.example.com/charges",
			Description: "One charge.",
			Scripts: &domain.Scripts{
				Pre: "console.log('pre')", PostOff: true, Post: "console.log('post')",
			},
		}},
	}

	data, err := Export(collection)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	back, err := imported(t, data)
	if err != nil {
		t.Fatalf("reading the export back: %v", err)
	}
	if back.Description != "Money moves here." || back.Scripts == nil ||
		back.Scripts.Pre != "vars.set('a', '1')" {
		t.Errorf("collection = %+v, want the description and the pre script", back)
	}
	node := back.Items[0]
	if node.Description != "One charge." || node.Scripts == nil {
		t.Fatalf("node = %+v, want the description and the scripts", node)
	}
	if node.Scripts.Pre != "console.log('pre')" || !node.Scripts.PostOff {
		t.Errorf("node scripts = %+v, want the pre script and the switched-off post one", node.Scripts)
	}
}
