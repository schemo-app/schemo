package request

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"

	"schemo/internal/domain"
)

var errGraphQLSchemaTooLarge = errors.New("the GraphQL schema exceeds the size limit")

// GraphQLSchema lists the operations of a schema, each with a query ready to insert.
type GraphQLSchema struct {
	Groups []GraphQLGroup `json:"groups"`
	// Descriptors are the merged type definitions, kept with the request so it opens offline.
	Descriptors string `json:"descriptors"`
	// Sources are the ones the schema was read from, each with what it gave.
	Sources []domain.SchemaSource `json:"sources"`
}

type GraphQLGroup struct {
	Name   string         `json:"name"`
	Fields []GraphQLField `json:"fields"`
}

type GraphQLField struct {
	Name      string `json:"name"`
	Signature string `json:"signature"`
	Query     string `json:"query,omitempty"`
	Variables string `json:"variables,omitempty"`
}

// DescribeGraphQLSchema lists the operations of definitions a request keeps, without reading its
// sources.
func (s *Drafts) DescribeGraphQLSchema(descriptors string) (GraphQLSchema, error) {
	if len(descriptors) > domain.MaxBodyBytes {
		return GraphQLSchema{}, errGraphQLSchemaTooLarge
	}
	var definitions graphQLDefinitions
	if err := json.Unmarshal([]byte(descriptors), &definitions); err != nil {
		return GraphQLSchema{}, fmt.Errorf("reading the stored GraphQL schema: %w", err)
	}
	schema := definitions.schema()
	schema.Descriptors = descriptors
	return schema, nil
}

// LoadGraphQLSchema reads every source again and merges their types; SDL files are parsed together so
// one may extend another's type.
func (s *Drafts) LoadGraphQLSchema(ctx context.Context, draft domain.Draft, sources []domain.SchemaSource) (GraphQLSchema, error) {
	files, err := newSchemaReader(s.files, len(sources))
	if err != nil {
		return GraphQLSchema{}, err
	}
	var merged graphQLDefinitions
	var sdl []string
	read := make([]domain.SchemaSource, len(sources))
	now := time.Now().UnixMilli()
	for i, source := range sources {
		source.LoadedAt, source.Methods, source.Files = now, 0, nil
		switch source.Kind {
		case domain.SchemaSourceServer:
			introspected, err := s.introspect(ctx, draft)
			if err != nil {
				return GraphQLSchema{}, err
			}
			if err := merged.merge(introspected); err != nil {
				return GraphQLSchema{}, err
			}
		case domain.SchemaSourceFile, domain.SchemaSourceFolder:
			paths, err := schemaSourceFiles(ctx, source, domain.ProtocolGraphQL)
			if err != nil {
				return GraphQLSchema{}, err
			}
			if source.Kind == domain.SchemaSourceFolder {
				for _, path := range paths {
					source.Files = append(source.Files, domain.SchemaFile{Name: schemaFileName(source.Path, path)})
				}
			}
			sdl = append(sdl, paths...)
		default:
			return GraphQLSchema{}, fmt.Errorf("unknown schema source %q", source.Kind)
		}
		read[i] = source
	}
	if len(sdl) > 0 {
		parsed, err := readGraphQLFiles(ctx, files, sdl)
		if err != nil {
			return GraphQLSchema{}, err
		}
		if err := merged.merge(parsed); err != nil {
			return GraphQLSchema{}, err
		}
	}
	slices.SortFunc(merged.Types, func(a, b graphQLDefinition) int { return strings.Compare(a.Name, b.Name) })
	encoded, err := json.Marshal(merged)
	if err != nil {
		return GraphQLSchema{}, err
	}
	if len(encoded) > domain.MaxBodyBytes {
		return GraphQLSchema{}, errGraphQLSchemaTooLarge
	}
	schema := merged.schema()
	schema.Descriptors, schema.Sources = string(encoded), read
	return schema, nil
}

func (s *Drafts) introspect(ctx context.Context, draft domain.Draft) (graphQLDefinitions, error) {
	input, err := s.schemaInput(ctx, draft)
	if err != nil {
		return graphQLDefinitions{}, err
	}
	payload, err := json.Marshal(graphQLPayload{Query: introspectionQuery})
	if err != nil {
		return graphQLDefinitions{}, err
	}
	response := s.engine.Do(ctx, Spec{
		Method: input.Method, URL: input.URL, Headers: input.Headers, Body: string(payload), Digest: input.Digest,
	})
	if response.Cancelled {
		return graphQLDefinitions{}, context.Canceled
	}
	if response.Error != "" {
		return graphQLDefinitions{}, fmt.Errorf("GraphQL introspection: %s", response.Error)
	}
	if response.Status < 200 || response.Status >= 300 {
		return graphQLDefinitions{}, fmt.Errorf("GraphQL introspection: %s", response.StatusText)
	}
	var result struct {
		Data struct {
			Schema *graphQLDefinitions `json:"__schema"`
		}
		Errors []struct{ Message string }
	}
	if err := json.Unmarshal([]byte(response.Body), &result); err != nil {
		return graphQLDefinitions{}, fmt.Errorf("reading GraphQL introspection: %w", err)
	}
	if result.Data.Schema == nil {
		if len(result.Errors) > 0 {
			return graphQLDefinitions{}, fmt.Errorf("GraphQL introspection: %s", result.Errors[0].Message)
		}
		return graphQLDefinitions{}, fmt.Errorf("GraphQL introspection returned no schema")
	}
	return *result.Data.Schema, nil
}

func readGraphQLFiles(ctx context.Context, files FileSource, paths []string) (graphQLDefinitions, error) {
	sources := make([]*ast.Source, 0, len(paths))
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return graphQLDefinitions{}, err
		}
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		seen[path] = true
		data, err := files.Read(path)
		if err != nil {
			return graphQLDefinitions{}, err
		}
		sources = append(sources, &ast.Source{Name: path, Input: string(data)})
	}
	parsed, err := gqlparser.LoadSchema(sources...)
	if err != nil {
		return graphQLDefinitions{}, err
	}
	definitions := graphQLDefinitions{
		QueryType: gqlRoot(parsed.Query), MutationType: gqlRoot(parsed.Mutation), SubscriptionType: gqlRoot(parsed.Subscription),
	}
	for _, definition := range parsed.Types {
		entry := graphQLDefinition{Name: definition.Name, Kind: string(definition.Kind)}
		for _, value := range definition.EnumValues {
			entry.EnumValues = append(entry.EnumValues, graphQLName{Name: value.Name})
		}
		if definition.IsAbstractType() {
			for _, member := range parsed.GetPossibleTypes(definition) {
				entry.PossibleTypes = append(entry.PossibleTypes, graphQLName{Name: member.Name})
			}
		}
		for _, field := range definition.Fields {
			// The parser adds the introspection fields to Query; a server's introspection omits them.
			if strings.HasPrefix(field.Name, "__") {
				continue
			}
			next := graphQLMember{Name: field.Name, Type: gqlTypeFromSDL(field.Type, parsed.Types)}
			for _, arg := range field.Arguments {
				next.Args = append(next.Args, graphQLMember{Name: arg.Name, Type: gqlTypeFromSDL(arg.Type, parsed.Types)})
			}
			entry.Fields = append(entry.Fields, next)
		}
		definitions.Types = append(definitions.Types, entry)
	}
	return definitions, nil
}

const introspectionQuery = `query SchemoSchema {
  __schema {
    queryType { name }
    mutationType { name }
    subscriptionType { name }
    types {
      kind name
      fields(includeDeprecated: true) { name type { ...TypeRef } args { name type { ...TypeRef } } }
      inputFields { name type { ...TypeRef } }
      enumValues(includeDeprecated: true) { name }
      possibleTypes { name }
    }
  }
}
fragment TypeRef on __Type {
  kind name ofType { kind name ofType { kind name ofType { kind name ofType {
    kind name ofType { kind name ofType { kind name }
  } } } } }
}`

func gqlRoot(definition *ast.Definition) *graphQLDefinition {
	if definition == nil {
		return nil
	}
	return &graphQLDefinition{Name: definition.Name}
}

func gqlTypeFromSDL(ref *ast.Type, types map[string]*ast.Definition) graphQLTypeRef {
	var out graphQLTypeRef
	if ref.Elem != nil {
		element := gqlTypeFromSDL(ref.Elem, types)
		out = graphQLTypeRef{Kind: "LIST", OfType: &element}
	} else {
		out.Name = ref.NamedType
		if definition := types[out.Name]; definition != nil {
			out.Kind = string(definition.Kind)
		}
	}
	if ref.NonNull {
		return graphQLTypeRef{Kind: "NON_NULL", OfType: &out}
	}
	return out
}
