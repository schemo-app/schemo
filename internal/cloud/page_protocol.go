package cloud

import (
	"encoding/json"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"

	"schemo/internal/domain"
)

type pageProtocol struct {
	schemes       []string
	defaultScheme string
	expandBase    func(string) string
}

var pageProtocols = map[domain.Protocol]pageProtocol{
	domain.ProtocolGRPC: {
		schemes:       []string{"http", "https", "grpc", "grpcs"},
		defaultScheme: "grpc",
		expandBase:    func(base string) string { return base },
	},
	domain.ProtocolWebSocket: {
		schemes:    []string{"ws", "wss"},
		expandBase: webSocketBase,
	},
}

func (p *page) writeEndpointProtocol(endpoint *snapshotv1.Endpoint, node domain.CollectionNode) {
	if node.Protocol != "" {
		endpoint.Protocol = new(snapshotv1.Protocol(node.Protocol))
	}
	if options := node.Options; options != nil {
		switch node.Protocol {
		case domain.ProtocolGraphQL:
			endpoint.Operation = publicText(maskText(options.OperationName, p.hidden), maxNameRunes)
			endpoint.Variables = publicVariables(options.Variables, p.hidden)
		case domain.ProtocolGRPC:
			endpoint.Operation = publicText(maskText(options.RPCMethod, p.hidden), maxTextRunes)
		}
	}
}

func readEndpointProtocol(from snapshotv1.Endpoint, node *domain.CollectionNode) {
	if from.Protocol == nil {
		return
	}
	node.Protocol = domain.Protocol(*from.Protocol)
	switch node.Protocol {
	case domain.ProtocolGraphQL:
		node.Options = &domain.ProtocolOptions{OperationName: optionalText(from.Operation), Variables: optionalText(from.Variables)}
	case domain.ProtocolGRPC:
		node.BodyKind = domain.BodyJSON
		node.Options = &domain.ProtocolOptions{RPCMethod: optionalText(from.Operation)}
	}
}

func optionalText(text *string) string {
	if text == nil {
		return ""
	}
	return *text
}

func publicText(text string, maximum int) *string {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > maximum {
		return nil
	}
	return &text
}

// publicVariables strips credential-like keys at any depth and masks references to hidden variables.
func publicVariables(text string, hidden map[string]domain.HiddenReason) *string {
	var values map[string]any
	reader := json.NewDecoder(strings.NewReader(text))
	reader.UseNumber()
	if err := reader.Decode(&values); err != nil || values == nil || reader.Decode(new(any)) != io.EOF {
		return nil
	}
	masked, err := json.Marshal(publicVariableValue(values, hidden))
	if err != nil {
		return nil
	}
	return publicText(string(masked), maxExampleRunes)
}

func publicVariableValue(value any, hidden map[string]domain.HiddenReason) any {
	switch value := value.(type) {
	case map[string]any:
		for key, item := range value {
			if looksLikeSecret(key) || maskText(key, hidden) != key {
				delete(value, key)
				continue
			}
			value[key] = publicVariableValue(item, hidden)
		}
	case []any:
		for index, item := range value {
			value[index] = publicVariableValue(item, hidden)
		}
	case string:
		return maskText(value, hidden)
	}
	return value
}

func (p *page) path(address string, protocol domain.Protocol) (string, error) {
	raw := strings.TrimSpace(address)
	for _, char := range raw {
		if char < 0x20 || char == 0x7f {
			return "", domain.Refuse(domain.CodeURLControlChar, domain.ErrNotAllowed, nil)
		}
	}
	config := pageProtocols[protocol]
	if config.defaultScheme != "" && raw != "" && !strings.Contains(raw, "://") && !strings.HasPrefix(raw, "{{") {
		raw = config.defaultScheme + "://" + raw
	}
	if scheme := schemeOf(raw); scheme != "" && !config.allows(scheme) {
		return "", domain.Refuse(domain.CodeProtocolUnsupported, domain.ErrNotAllowed,
			domain.Args{"scheme": scheme})
	}

	baseVariable := "{{" + domain.BaseURLVariable + "}}"
	path := raw
	if config.expandBase == nil {
		path = strings.TrimPrefix(strings.TrimPrefix(path, baseVariable), p.base)
	} else if suffix, usesBase := strings.CutPrefix(path, baseVariable); usesBase && p.base != "" && p.hidden[domain.BaseURLVariable] == "" {
		path = config.expandBase(p.base) + suffix
	}
	if utf8.RuneCountInString(path) > maxTextRunes {
		return "", domain.Refuse(domain.CodeURLTooLong, domain.ErrNotAllowed,
			domain.Args{"max": strconv.Itoa(maxTextRunes)})
	}
	return maskText(path, p.hidden), nil
}

func (p pageProtocol) allows(scheme string) bool {
	if len(p.schemes) == 0 {
		return scheme == "http" || scheme == "https"
	}
	return slices.Contains(p.schemes, scheme)
}

func webSocketBase(base string) string {
	if rest, found := strings.CutPrefix(base, "https://"); found {
		return "wss://" + rest
	}
	if rest, found := strings.CutPrefix(base, "http://"); found {
		return "ws://" + rest
	}
	return base
}
