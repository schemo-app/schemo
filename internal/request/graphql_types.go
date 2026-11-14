package request

import (
	"fmt"
	"slices"
)

type graphQLTypeRef struct {
	Kind, Name string
	OfType     *graphQLTypeRef
}

func (ref graphQLTypeRef) String() string {
	switch {
	case ref.Kind == "NON_NULL" && ref.OfType != nil:
		return ref.OfType.String() + "!"
	case ref.Kind == "LIST" && ref.OfType != nil:
		return "[" + ref.OfType.String() + "]"
	default:
		return ref.Name
	}
}

func (ref graphQLTypeRef) named() string {
	for ref.OfType != nil {
		ref = *ref.OfType
	}
	return ref.Name
}

func (ref graphQLTypeRef) composite() bool {
	for ref.OfType != nil {
		ref = *ref.OfType
	}
	return ref.Kind == "OBJECT" || ref.Kind == "INTERFACE" || ref.Kind == "UNION"
}

type graphQLMember struct {
	Name string
	Type graphQLTypeRef
	Args []graphQLMember
}

type graphQLName struct{ Name string }

type graphQLDefinition struct {
	Name, Kind    string
	Fields        []graphQLMember
	InputFields   []graphQLMember
	EnumValues    []graphQLName
	PossibleTypes []graphQLName
}

type graphQLDefinitions struct {
	QueryType, MutationType, SubscriptionType *graphQLDefinition
	Types                                     []graphQLDefinition
}

// merge combines definitions without replacing a type or field with a conflicting signature.
func (definitions *graphQLDefinitions) merge(other graphQLDefinitions) error {
	for _, roots := range [][2]*graphQLDefinition{
		{definitions.QueryType, other.QueryType},
		{definitions.MutationType, other.MutationType},
		{definitions.SubscriptionType, other.SubscriptionType},
	} {
		if roots[0] != nil && roots[1] != nil && roots[0].Name != roots[1].Name {
			return fmt.Errorf("conflicting GraphQL operation roots %q and %q", roots[0].Name, roots[1].Name)
		}
	}
	if definitions.QueryType == nil {
		definitions.QueryType = other.QueryType
	}
	if definitions.MutationType == nil {
		definitions.MutationType = other.MutationType
	}
	if definitions.SubscriptionType == nil {
		definitions.SubscriptionType = other.SubscriptionType
	}
	known := map[string]int{}
	for i, definition := range definitions.Types {
		known[definition.Name] = i
	}
	for _, definition := range other.Types {
		at, ok := known[definition.Name]
		if !ok {
			known[definition.Name] = len(definitions.Types)
			definitions.Types = append(definitions.Types, definition)
			continue
		}
		into := &definitions.Types[at]
		if into.Kind != definition.Kind {
			return fmt.Errorf("conflicting GraphQL type %q", into.Name)
		}
		var err error
		into.Fields, err = mergeGraphQLFields(into.Name, into.Fields, definition.Fields)
		if err != nil {
			return err
		}
		into.InputFields, err = mergeGraphQLFields(into.Name, into.InputFields, definition.InputFields)
		if err != nil {
			return err
		}
		into.EnumValues = mergeByName(into.EnumValues, definition.EnumValues, func(name graphQLName) string { return name.Name })
		into.PossibleTypes = mergeByName(into.PossibleTypes, definition.PossibleTypes, func(name graphQLName) string { return name.Name })
	}
	return nil
}

func mergeGraphQLFields(typeName string, fields, more []graphQLMember) ([]graphQLMember, error) {
	known := make(map[string]graphQLMember, len(fields))
	for _, field := range fields {
		known[field.Name] = field
	}
	for _, field := range more {
		previous, ok := known[field.Name]
		if !ok {
			fields = append(fields, field)
			known[field.Name] = field
			continue
		}
		if previous.Type.String() != field.Type.String() || !sameGraphQLArguments(previous.Args, field.Args) {
			return nil, fmt.Errorf("conflicting GraphQL field %s.%s", typeName, field.Name)
		}
	}
	return fields, nil
}

func sameGraphQLArguments(first, second []graphQLMember) bool {
	if len(first) != len(second) {
		return false
	}
	signatures := make(map[string]string, len(first))
	for _, arg := range first {
		signatures[arg.Name] = arg.Type.String()
	}
	for _, arg := range second {
		if signatures[arg.Name] != arg.Type.String() {
			return false
		}
	}
	return true
}

func mergeByName[T any](into, more []T, name func(T) string) []T {
	for _, entry := range more {
		if !slices.ContainsFunc(into, func(have T) bool { return name(have) == name(entry) }) {
			into = append(into, entry)
		}
	}
	return into
}
