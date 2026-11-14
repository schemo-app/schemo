package request

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

func writeProtos(t *testing.T, root string, sources map[string]string) {
	t.Helper()
	for name, contents := range sources {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func fileSource(path string) domain.SchemaSource {
	return domain.SchemaSource{Kind: domain.SchemaSourceFile, Path: path}
}

func folderSource(path string, skipped ...string) domain.SchemaSource {
	return domain.SchemaSource{Kind: domain.SchemaSourceFolder, Path: path, Skipped: skipped}
}

func loadProtos(t *testing.T, sources ...domain.SchemaSource) (RPCSchema, error) {
	return (&Drafts{files: platform.NewFileReader()}).LoadRPCSchema(t.Context(), domain.Draft{}, sources)
}

func loadGraphQL(t *testing.T, sources ...domain.SchemaSource) (GraphQLSchema, error) {
	return (&Drafts{files: platform.NewFileReader()}).LoadGraphQLSchema(t.Context(), domain.Draft{}, sources)
}

func rpcServices(schema RPCSchema) []string {
	var services []string
	for _, method := range schema.Methods {
		services = append(services, method.Service+"/"+method.Name)
	}
	return services
}

var moduleProtos = map[string]string{
	"common/v1/access.proto": `syntax = "proto3"; package common.v1; message Access { string org = 1; }`,
	"billing/v1/billing.proto": `syntax = "proto3"; package billing.v1; import "common/v1/access.proto";
		import "google/protobuf/timestamp.proto";
		service Billing { rpc Read(common.v1.Access) returns (google.protobuf.Timestamp); }`,
	"billing/v1/offers.proto": `syntax = "proto3"; package billing.v1; import "common/v1/access.proto";
		service Offers { rpc List(common.v1.Access) returns (common.v1.Access); }`,
}

func TestProtoFileResolvesImportsFromTheModuleRoot(t *testing.T) {
	root := t.TempDir()
	writeProtos(t, root, moduleProtos)
	schema, err := loadProtos(t, fileSource(filepath.Join(root, "billing", "v1", "billing.proto")))
	if err != nil || strings.Join(rpcServices(schema), " ") != "billing.v1.Billing/Read" {
		t.Fatalf("schema = %+v, %v", schema, err)
	}
	if source := schema.Sources[0]; source.Methods != 1 || source.LoadedAt == 0 || source.Files != nil {
		t.Fatalf("source = %+v", source)
	}
}

func TestProtoFolderNamesWhatEachFileDeclares(t *testing.T) {
	root := t.TempDir()
	writeProtos(t, root, moduleProtos)
	// A copy in a hidden folder would declare billing.v1.Billing a second time.
	writeProtos(t, root, map[string]string{".cache/billing.proto": moduleProtos["billing/v1/billing.proto"]})
	schema, err := loadProtos(t, folderSource(filepath.Join(root, "billing")))
	if err != nil || strings.Join(rpcServices(schema), " ") != "billing.v1.Billing/Read billing.v1.Offers/List" {
		t.Fatalf("schema = %+v, %v", schema, err)
	}
	want := []domain.SchemaFile{{Name: "v1/billing.proto", Methods: 1}, {Name: "v1/offers.proto", Methods: 1}}
	if source := schema.Sources[0]; source.Methods != 2 || !slices.Equal(source.Files, want) {
		t.Fatalf("source = %+v", source)
	}
	whole, err := loadProtos(t, folderSource(root))
	if err != nil || len(whole.Sources[0].Files) != 3 {
		t.Fatalf("module root: %+v, %v", whole.Sources, err)
	}
}

func TestProtoFolderLeavesOutTheSkippedFiles(t *testing.T) {
	root := t.TempDir()
	writeProtos(t, root, moduleProtos)
	schema, err := loadProtos(t, folderSource(filepath.Join(root, "billing"), "v1/offers.proto"))
	if err != nil || strings.Join(rpcServices(schema), " ") != "billing.v1.Billing/Read" || len(schema.Sources[0].Files) != 1 {
		t.Fatalf("schema = %+v, %v", schema, err)
	}
}

func TestProtoFolderWithoutProtosIsRefused(t *testing.T) {
	if _, err := loadProtos(t, folderSource(t.TempDir())); err == nil || !strings.Contains(err.Error(), "no .proto files") {
		t.Fatalf("err = %v", err)
	}
}

// A file of shared messages is a source like any other: the list says it gives no methods.
func TestProtoSourceWithoutServicesGivesNoMethods(t *testing.T) {
	root := t.TempDir()
	writeProtos(t, root, moduleProtos)
	schema, err := loadProtos(t, fileSource(filepath.Join(root, "common", "v1", "access.proto")))
	if err != nil || len(schema.Methods) != 0 || schema.Sources[0].Methods != 0 {
		t.Fatalf("schema = %+v, %v", schema, err)
	}
}

func TestProtoSourcesMergeIntoOneSchema(t *testing.T) {
	root := t.TempDir()
	writeProtos(t, root, moduleProtos)
	schema, err := loadProtos(t,
		fileSource(filepath.Join(root, "billing", "v1", "billing.proto")),
		fileSource(filepath.Join(root, "billing", "v1", "offers.proto")))
	if err != nil || len(schema.Methods) != 2 || schema.Sources[0].Methods != 1 || schema.Sources[1].Methods != 1 {
		t.Fatalf("schema = %+v, %v", schema, err)
	}
	stored, err := (&Drafts{}).DescribeRPCSchema(schema.Descriptors)
	if err != nil || !slices.Equal(rpcServices(stored), rpcServices(schema)) {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
}

func TestProtoSourcesReportConflictingFileNames(t *testing.T) {
	root := t.TempDir()
	writeProtos(t, root, map[string]string{
		"a/api.proto": `syntax = "proto3"; package a; message M {} service A { rpc Call(M) returns (M); }`,
		"b/api.proto": `syntax = "proto3"; package b; message M {} service B { rpc Call(M) returns (M); }`,
	})
	_, err := loadProtos(t,
		fileSource(filepath.Join(root, "a", "api.proto")),
		fileSource(filepath.Join(root, "b", "api.proto")))
	if err == nil || !strings.Contains(err.Error(), `conflicting definitions for proto file "api.proto"`) {
		t.Fatalf("loading two distinct api.proto definitions returned %v", err)
	}
	schema, err := loadProtos(t, folderSource(root))
	if err != nil || !slices.Equal(rpcServices(schema), []string{"a.A/Call", "b.B/Call"}) {
		t.Fatalf("loading both files under a common root: %v, %v", rpcServices(schema), err)
	}
}

func TestProtoSyntaxErrorIsReportedWithoutSearchingParents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.proto")
	if err := os.WriteFile(path, []byte(`syntax = "proto3"; message {`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadProtos(t, fileSource(path)); err == nil || !strings.Contains(err.Error(), "broken.proto") {
		t.Fatalf("err = %v", err)
	}
}

func TestProtoFileCarriesItsDependencies(t *testing.T) {
	root := t.TempDir()
	writeProtos(t, root, map[string]string{
		"messages.proto": `syntax = "proto3"; package test; message Item { int64 id = 1; }`,
		"api.proto": `syntax = "proto3"; package test; import "messages.proto";
			service Catalog { rpc Read(Item) returns (Item); }`,
	})
	schema, err := loadProtos(t, fileSource(filepath.Join(root, "api.proto")))
	if err != nil || len(schema.Methods) != 1 {
		t.Fatalf("schema = %+v, %v", schema, err)
	}
	method := schema.Methods[0]
	if method.Service != "test.Catalog" || method.Name != "Read" || method.Example != "{\n  \"id\": \"0\"\n}" {
		t.Errorf("method = %+v", method)
	}
}

func TestReadingProtoSourcesAgainPicksUpEditsOnDisk(t *testing.T) {
	root := t.TempDir()
	writeProtos(t, root, moduleProtos)
	for _, source := range []domain.SchemaSource{
		fileSource(filepath.Join(root, "billing", "v1", "offers.proto")), folderSource(filepath.Join(root, "billing")),
	} {
		writeProtos(t, root, map[string]string{"billing/v1/offers.proto": moduleProtos["billing/v1/offers.proto"]})
		before, err := loadProtos(t, source)
		if err != nil {
			t.Fatalf("%s: %v", source.Path, err)
		}
		writeProtos(t, root, map[string]string{"billing/v1/offers.proto": `syntax = "proto3"; package billing.v1;
			import "common/v1/access.proto";
			service Offers {
				rpc List(common.v1.Access) returns (common.v1.Access);
				rpc Accept(common.v1.Access) returns (common.v1.Access);
			}`})
		after, err := loadProtos(t, source)
		if err != nil || len(after.Methods) != len(before.Methods)+1 {
			t.Fatalf("%s: before %d methods, after %+v, %v", source.Path, len(before.Methods), after.Methods, err)
		}
	}
	if _, err := loadProtos(t, fileSource(filepath.Join(root, "gone.proto"))); domain.CodeOf(err) != domain.CodeFileMissing {
		t.Fatalf("err = %v", err)
	}
}

var splitGraphQL = map[string]string{
	"schema/query.graphql":  `type Query { user(id: ID!): User }`,
	"schema/user.graphqls":  `type User { id: ID! name: String }`,
	"schema/notes.txt":      `not a schema`,
	"schema/.old/user.gql":  `type User { id: ID! }`,
	"schema/types/post.gql": `type Post { title: String } extend type Query { posts: [Post] }`,
}

func graphQLFields(schema GraphQLSchema, group string) []string {
	for _, entry := range schema.Groups {
		if entry.Name == group {
			names := make([]string, len(entry.Fields))
			for i, field := range entry.Fields {
				names[i] = field.Name
			}
			return names
		}
	}
	return nil
}

func TestGraphQLFolderJoinsEveryFileUnderIt(t *testing.T) {
	root := t.TempDir()
	writeProtos(t, root, splitGraphQL)
	schema, err := loadGraphQL(t, folderSource(filepath.Join(root, "schema")))
	if err != nil {
		t.Fatal(err)
	}
	if queries := strings.Join(graphQLFields(schema, "query"), " "); queries != "user posts" {
		t.Fatalf("queries = %q", queries)
	}
	want := []domain.SchemaFile{{Name: "query.graphql"}, {Name: "types/post.gql"}, {Name: "user.graphqls"}}
	if !slices.Equal(schema.Sources[0].Files, want) {
		t.Fatalf("files = %+v", schema.Sources[0].Files)
	}
	if _, err := loadGraphQL(t, folderSource(t.TempDir())); err == nil || !strings.Contains(err.Error(), "no .graphql") {
		t.Fatalf("empty folder: %v", err)
	}
}

// The server's schema and a file are read apart, so each may declare Query in full.
func TestGraphQLSourcesMergeTheTypesTheyShare(t *testing.T) {
	root := t.TempDir()
	writeProtos(t, root, map[string]string{
		"live.graphql":  `type Query { me: User } type User { id: ID! }`,
		"draft.graphql": `type Query { me: User drafts: [String] } type User { id: ID! email: String }`,
	})
	live, err := readGraphQLFiles(t.Context(), platform.NewFileReader(), []string{filepath.Join(root, "live.graphql")})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := readGraphQLFiles(t.Context(), platform.NewFileReader(), []string{filepath.Join(root, "draft.graphql")})
	if err != nil {
		t.Fatal(err)
	}
	if err := live.merge(draft); err != nil {
		t.Fatal(err)
	}
	schema := live.schema()
	if queries := strings.Join(graphQLFields(schema, "query"), " "); queries != "me drafts" {
		t.Fatalf("queries = %q", queries)
	}
	if me := graphQLOperation(t, schema, "me").Query; !strings.Contains(me, "email") {
		t.Fatalf("me =\n%s", me)
	}
}

func TestStoredGraphQLSchemaOpensWithoutItsSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shop.graphql")
	if err := os.WriteFile(path, []byte(shopSDL), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadGraphQL(t, fileSource(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	stored, err := (&Drafts{}).DescribeGraphQLSchema(loaded.Descriptors)
	if err != nil || graphQLOperation(t, stored, "product").Query != graphQLOperation(t, loaded, "product").Query {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
}

func TestReadingGraphQLSourcesAgainPicksUpEditsOnDisk(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "query.graphql")
	writeProtos(t, root, map[string]string{"query.graphql": `type Query { me: String }`})
	before, err := loadGraphQL(t, fileSource(file))
	if err != nil || len(graphQLFields(before, "query")) != 1 {
		t.Fatalf("before = %+v, %v", before, err)
	}
	writeProtos(t, root, map[string]string{"query.graphql": `type Query { me: String version: Int }`})
	after, err := loadGraphQL(t, fileSource(file))
	if err != nil || len(graphQLFields(after, "query")) != 2 {
		t.Fatalf("after = %+v, %v", after, err)
	}
	if _, err := loadGraphQL(t, fileSource(filepath.Join(root, "gone.graphql"))); domain.CodeOf(err) != domain.CodeFileMissing {
		t.Fatalf("err = %v", err)
	}
}

const shopSDL = `
type Query {
	product(id: ID!, filter: Filter!): Product
	search(text: String): [Result!]!
}
type Product {
	id: ID!
	name: String
	seller: Seller
	related: [Product]
	reviews(first: Int!): [Review]
}
type Seller { name: String rating: Rating address: Address }
type Address { city: String country: Country }
type Country { code: String region: Region }
type Region { name: String }
type Review { text: String }
union Result = Product | Seller
enum Rating { GOOD BAD }
input Filter { rating: Rating! tags: [String] inner: Filter }
`

func shopSchema(t *testing.T) GraphQLSchema {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shop.graphql")
	if err := os.WriteFile(path, []byte(shopSDL), 0o600); err != nil {
		t.Fatal(err)
	}
	schema, err := loadGraphQL(t, fileSource(path))
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func graphQLOperation(t *testing.T, schema GraphQLSchema, name string) GraphQLField {
	t.Helper()
	for _, group := range schema.Groups {
		for _, field := range group.Fields {
			if field.Name == name {
				return field
			}
		}
	}
	t.Fatalf("no operation %s", name)
	return GraphQLField{}
}

func TestInsertedGraphQLQueryAsksForTheFieldsOfTheAnswer(t *testing.T) {
	product := graphQLOperation(t, shopSchema(t), "product")
	want := `query product($id: ID!, $filter: Filter!) {
  product(id: $id, filter: $filter) {
    id
    name
    seller {
      name
      rating
      address {
        city
      }
    }
  }
}`
	// related returns the type already open, reviews needs an argument, and country lies past the
	// depth limit.
	if product.Query != want {
		t.Fatalf("query =\n%s", product.Query)
	}
	var variables map[string]any
	if err := json.Unmarshal([]byte(product.Variables), &variables); err != nil {
		t.Fatal(err)
	}
	filter, _ := variables["filter"].(map[string]any)
	if filter["rating"] != "GOOD" || filter["inner"] == nil {
		t.Fatalf("variables = %s", product.Variables)
	}
}

func TestInsertedGraphQLQueryTakesOptionalArgumentsAsVariables(t *testing.T) {
	search := graphQLOperation(t, shopSchema(t), "search")
	if !strings.HasPrefix(search.Query, "query search($text: String) {\n  search(text: $text) {") {
		t.Fatalf("query =\n%s", search.Query)
	}
	if search.Variables != "{\n  \"text\": \"\"\n}" {
		t.Fatalf("variables = %q", search.Variables)
	}
}

func TestInsertedGraphQLQueryNamesTheMembersOfAUnion(t *testing.T) {
	search := graphQLOperation(t, shopSchema(t), "search").Query
	for _, part := range []string{"__typename", "... on Product {", "... on Seller {"} {
		if !strings.Contains(search, part) {
			t.Fatalf("query lacks %q:\n%s", part, search)
		}
	}
}
