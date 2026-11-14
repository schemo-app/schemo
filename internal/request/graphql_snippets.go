package request

import (
	"encoding/json"
	"slices"
	"strings"
)

// graphQLIndex holds the types a selection can go into: scalars and introspection types have no
// fields to ask for.
type graphQLIndex map[string]*graphQLDefinition

func (definitions graphQLDefinitions) schema() GraphQLSchema {
	slices.SortFunc(definitions.Types, func(a, b graphQLDefinition) int { return strings.Compare(a.Name, b.Name) })
	index := graphQLIndex{}
	for i, definition := range definitions.Types {
		if definition.Kind != "SCALAR" && !strings.HasPrefix(definition.Name, "__") {
			index[definition.Name] = &definitions.Types[i]
		}
	}
	schema := GraphQLSchema{Groups: []GraphQLGroup{}}
	roots := []struct {
		operation string
		root      *graphQLDefinition
	}{
		{"query", definitions.QueryType}, {"mutation", definitions.MutationType}, {"subscription", definitions.SubscriptionType},
	}
	for _, root := range roots {
		if root.root == nil || index[root.root.Name] == nil {
			continue
		}
		group := GraphQLGroup{Name: root.operation, Fields: []GraphQLField{}}
		for _, field := range index[root.root.Name].Fields {
			if !strings.HasPrefix(field.Name, "__") {
				group.Fields = append(group.Fields, index.snippet(field, root.operation))
			}
		}
		schema.Groups = append(schema.Groups, group)
	}
	return schema
}

func describeField(field graphQLMember) GraphQLField {
	var arguments []string
	for _, arg := range field.Args {
		arguments = append(arguments, arg.Name+": "+arg.Type.String())
	}
	return GraphQLField{Name: field.Name, Signature: gqlArguments(arguments) + ": " + field.Type.String()}
}

func (index graphQLIndex) snippet(field graphQLMember, operation string) GraphQLField {
	entry := describeField(field)
	var variables, uses []string
	values := map[string]any{}
	// Optional arguments become variables too: they are what a query is usually tuned with, paging
	// and filters, and one removed from the JSON leaves its argument unset.
	for _, arg := range field.Args {
		variables = append(variables, "$"+arg.Name+": "+arg.Type.String())
		uses = append(uses, arg.Name+": $"+arg.Name)
		values[arg.Name] = index.example(arg.Type, map[string]bool{})
	}
	selection := field.Name + gqlArguments(uses)
	if field.Type.composite() {
		name := field.Type.named()
		fields := index.selection(name, 1, map[string]bool{name: true})
		if fields == "" {
			fields = "{\n    __typename\n  }"
		}
		selection += " " + fields
	}
	entry.Query = operation + " " + field.Name + gqlArguments(variables) + " {\n  " + selection + "\n}"
	if len(values) != 0 {
		example, _ := json.MarshalIndent(values, "", "  ")
		entry.Variables = string(example)
	}
	return entry
}

// A query inserted from the schema asks for nested objects this many levels down: enough to show
// the shape of the answer without pulling the whole graph.
const graphQLSelectionDepth = 3

// selection lists a type's fields down to graphQLSelectionDepth, a fragment per union member; types
// already open are skipped, ending cycles.
func (index graphQLIndex) selection(name string, depth int, open map[string]bool) string {
	definition := index[name]
	if definition == nil {
		return ""
	}
	indent := strings.Repeat("  ", depth+1)
	var lines []string
	switch definition.Kind {
	case "OBJECT", "INTERFACE":
		for _, field := range definition.Fields {
			if line := index.selectField(field, depth, open); line != "" {
				lines = append(lines, indent+line)
			}
		}
	case "UNION":
		lines = append(lines, indent+"__typename")
		for _, member := range definition.PossibleTypes {
			if fields := index.selection(member.Name, depth+1, open); fields != "" {
				lines = append(lines, indent+"... on "+member.Name+" "+fields)
			}
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "{\n" + strings.Join(lines, "\n") + "\n" + strings.Repeat("  ", depth) + "}"
}

func (index graphQLIndex) selectField(field graphQLMember, depth int, open map[string]bool) string {
	if strings.HasPrefix(field.Name, "__") {
		return ""
	}
	// A required argument has no value to give inside a generated selection.
	for _, arg := range field.Args {
		if arg.Type.Kind == "NON_NULL" {
			return ""
		}
	}
	if !field.Type.composite() {
		return field.Name
	}
	name := field.Type.named()
	if depth >= graphQLSelectionDepth || open[name] {
		return ""
	}
	open[name] = true
	defer delete(open, name)
	if fields := index.selection(name, depth+1, open); fields != "" {
		return field.Name + " " + fields
	}
	return ""
}

func gqlArguments(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return "(" + strings.Join(values, ", ") + ")"
}

// example is a placeholder for a variable. An input object is filled field by field, so its shape
// shows without opening the type.
func (index graphQLIndex) example(ref graphQLTypeRef, open map[string]bool) any {
	switch {
	case ref.Kind == "NON_NULL" && ref.OfType != nil:
		return index.example(*ref.OfType, open)
	case ref.Kind == "LIST":
		return []any{}
	}
	switch ref.Name {
	case "Int", "Float":
		return 0
	case "Boolean":
		return false
	}
	definition := index[ref.Name]
	switch {
	case definition == nil:
		return ""
	case definition.Kind == "ENUM" && len(definition.EnumValues) > 0:
		return definition.EnumValues[0].Name
	case definition.Kind == "INPUT_OBJECT":
		value := map[string]any{}
		if open[ref.Name] {
			return value
		}
		open[ref.Name] = true
		defer delete(open, ref.Name)
		for _, field := range append(slices.Clone(definition.InputFields), definition.Fields...) {
			value[field.Name] = index.example(field.Type, open)
		}
		return value
	}
	return ""
}
