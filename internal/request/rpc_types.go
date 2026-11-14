package request

import (
	"encoding/json"

	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

type rpcResolver struct {
	*protoregistry.Files
	*dynamicpb.Types
}

func newRPCResolver(set *descriptorpb.FileDescriptorSet) (*rpcResolver, error) {
	files, err := protodesc.NewFiles(set)
	if err != nil {
		return nil, err
	}
	return &rpcResolver{files, dynamicpb.NewTypes(files)}, nil
}

func rpcExample(message protoreflect.MessageDescriptor) string {
	fields := map[string]any{}
	for i := 0; i < message.Fields().Len(); i++ {
		field := message.Fields().Get(i)
		if oneof := field.ContainingOneof(); oneof != nil && oneof.Fields().Get(0) != field {
			continue
		}
		var value any
		switch {
		case field.IsMap():
			value = map[string]any{}
		case field.IsList():
			value = []any{}
		default:
			switch field.Kind() {
			case protoreflect.MessageKind, protoreflect.GroupKind:
				continue
			case protoreflect.EnumKind:
				value = field.Enum().Values().Get(0).Name()
			case protoreflect.BoolKind:
				value = false
			case protoreflect.StringKind, protoreflect.BytesKind:
				value = ""
			case protoreflect.Int64Kind, protoreflect.Uint64Kind, protoreflect.Sint64Kind,
				protoreflect.Fixed64Kind, protoreflect.Sfixed64Kind:
				value = "0"
			default:
				value = 0
			}
		}
		fields[field.JSONName()] = value
	}
	data, _ := json.MarshalIndent(fields, "", "  ")
	return string(data)
}
