package request

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"

	"schemo/internal/domain"
)

type graphQLPayload struct {
	Query         string          `json:"query"`
	Variables     json.RawMessage `json:"variables,omitempty"`
	OperationName string          `json:"operationName,omitempty"`
}

func graphQLBody(input parts, masked bool) (string, error) {
	var variables json.RawMessage
	if text := strings.TrimSpace(input.options.Variables); text != "" {
		variables = json.RawMessage(text)
		var object map[string]json.RawMessage
		if err := json.Unmarshal(variables, &object); err != nil || object == nil {
			if !masked {
				return "", errors.New("GraphQL variables must be a JSON object")
			}
			// A masked token can replace an entire object. Keep it as replayable text.
			variables, _ = json.Marshal(input.options.Variables)
		}
	}
	payload, err := json.Marshal(graphQLPayload{
		Query: input.body, Variables: variables, OperationName: input.options.OperationName,
	})
	return string(payload), err
}

func graphQLReplayOptions(options domain.ProtocolOptions) *domain.ReplayOptions {
	return &domain.ReplayOptions{Variables: options.Variables, OperationName: options.OperationName}
}

func graphQLRecord(body string, in SendInput) (string, *domain.ReplayOptions, string) {
	query, options := graphQLReplay(body)
	if options == nil {
		query, options = graphQLReplay(in.MaskedBody)
	}
	if options != nil && options.OperationName != "" {
		return query, options, options.OperationName
	}
	doc, err := parser.ParseQuery(&ast.Source{Input: query})
	if err == nil && len(doc.Operations) == 1 {
		return query, options, doc.Operations[0].Name
	}
	return query, options, ""
}

func graphQLReplay(body string) (string, *domain.ReplayOptions) {
	var payload graphQLPayload
	if json.Unmarshal([]byte(body), &payload) != nil {
		return "", nil
	}
	options := &domain.ReplayOptions{Variables: string(payload.Variables), OperationName: payload.OperationName}
	if len(payload.Variables) > 0 && payload.Variables[0] == '"' {
		_ = json.Unmarshal(payload.Variables, &options.Variables)
	}
	return payload.Query, options
}
