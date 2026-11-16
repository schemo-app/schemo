package request

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	"schemo/internal/domain"
)

type RPCSchema struct {
	Descriptors string      `json:"descriptors"`
	Methods     []RPCMethod `json:"methods"`
	// Warnings name the services reflection listed but could not describe.
	Warnings []string `json:"warnings,omitempty"`
	// Sources are the ones the schema was read from, each with what it gave.
	Sources []domain.SchemaSource `json:"sources"`
}

type RPCMethod struct {
	ClientStreaming bool   `json:"clientStreaming,omitempty"`
	Service         string `json:"service"`
	Name            string `json:"name"`
	Input           string `json:"input"`
	Output          string `json:"output"`
	Example         string `json:"example"`
	Streaming       bool   `json:"streaming"`
}

// DescribeRPCSchema lists the methods of descriptors a request keeps, without reading its sources.
func (s *Drafts) DescribeRPCSchema(descriptors string) (RPCSchema, error) {
	return describeRPC(descriptors)
}

// LoadRPCSchema reads every source again, the server by reflection and files from disk, and merges
// what they describe into one schema.
func (s *Drafts) LoadRPCSchema(ctx context.Context, draft domain.Draft, sources []domain.SchemaSource) (RPCSchema, error) {
	filesOnDisk, err := newSchemaReader(s.files, len(sources))
	if err != nil {
		return RPCSchema{}, err
	}
	var files []protoreflect.FileDescriptor
	var warnings []string
	read := make([]domain.SchemaSource, len(sources))
	now := time.Now().UnixMilli()
	for i, source := range sources {
		var given []protoreflect.FileDescriptor
		var err error
		switch source.Kind {
		case domain.SchemaSourceServer:
			var unresolved []string
			given, unresolved, err = s.reflectSchema(ctx, draft)
			warnings = append(warnings, unresolved...)
			source.Files = nil
		case domain.SchemaSourceFile, domain.SchemaSourceFolder:
			given, source.Files, err = compileProtoSource(ctx, filesOnDisk, source)
		default:
			err = fmt.Errorf("unknown schema source %q", source.Kind)
		}
		if err != nil {
			return RPCSchema{}, err
		}
		source.Methods, source.LoadedAt = rpcMethodCount(given...), now
		read[i] = source
		files = append(files, given...)
	}
	encoded, err := encodeRPCDescriptors(files)
	if err != nil {
		return RPCSchema{}, err
	}
	schema, err := describeRPC(encoded)
	if err != nil {
		return RPCSchema{}, err
	}
	schema.Warnings, schema.Sources = warnings, read
	return schema, nil
}

func (s *Drafts) reflectSchema(ctx context.Context, draft domain.Draft) ([]protoreflect.FileDescriptor, []string, error) {
	input, err := s.schemaInput(ctx, draft)
	if err != nil {
		return nil, nil, err
	}
	cfg := s.engine.net.settings()
	ctx, cancel := requestDeadline(ctx, cfg.Requests.Timeout)
	defer cancel()
	ctx, err = rpcContext(ctx, input.Headers)
	if err != nil {
		return nil, nil, err
	}
	client, err := rpcClient(input.URL, cfg)
	if err != nil {
		return nil, nil, err
	}
	defer client.Close()
	files, warnings, err := reflectServices(ctx, client)
	if err != nil {
		return nil, nil, withRPCHint(err, input.URL)
	}
	return files, warnings, nil
}

func rpcMethodCount(files ...protoreflect.FileDescriptor) int {
	count := 0
	for _, file := range files {
		for i := 0; i < file.Services().Len(); i++ {
			count += file.Services().Get(i).Methods().Len()
		}
	}
	return count
}

// compileProtoSource compiles a .proto file, or every one under a folder but the skipped ones,
// and names what each file of a folder declares.
func compileProtoSource(ctx context.Context, files FileSource, source domain.SchemaSource) ([]protoreflect.FileDescriptor, []domain.SchemaFile, error) {
	paths, err := schemaSourceFiles(ctx, source, domain.ProtocolGRPC)
	if err != nil {
		return nil, nil, err
	}
	if source.Kind == domain.SchemaSourceFile {
		compiled, err := compileProtos(ctx, files, filepath.Dir(source.Path), paths)
		return compiled, nil, err
	}
	compiled, err := compileProtos(ctx, files, source.Path, paths)
	if err != nil {
		return nil, nil, err
	}
	listed := make([]domain.SchemaFile, len(paths))
	for i, path := range paths {
		listed[i] = domain.SchemaFile{Name: schemaFileName(source.Path, path), Methods: rpcMethodCount(compiled[i])}
	}
	return compiled, listed, nil
}

// compileProtos tries the start folder, then each parent, as the -I root. Only a missing import moves
// on; other errors return at once. Files come back in path order.
func compileProtos(ctx context.Context, files FileSource, start string, paths []string) ([]protoreflect.FileDescriptor, error) {
	var first error
	for root := start; ; {
		compiled, err := compileProtosFrom(ctx, files, root, paths)
		if err == nil || !errors.Is(err, domain.ErrNotFound) {
			return compiled, err
		}
		if first == nil {
			first = err
		}
		parent := filepath.Dir(root)
		if parent == root {
			return nil, first
		}
		root = parent
	}
}

func compileProtosFrom(ctx context.Context, files FileSource, root string, paths []string) ([]protoreflect.FileDescriptor, error) {
	names := make([]string, len(paths))
	for i, path := range paths {
		name, err := filepath.Rel(root, path)
		if err != nil {
			return nil, err
		}
		names[i] = filepath.ToSlash(name)
	}
	compiler := protocompile.Compiler{Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
		Accessor: func(name string) (io.ReadCloser, error) {
			return protoSource(files, root, name)
		},
	})}
	compiled, err := compiler.Compile(ctx, names...)
	if err != nil {
		return nil, err
	}
	descriptors := make([]protoreflect.FileDescriptor, len(compiled))
	for i, file := range compiled {
		descriptors[i] = file
	}
	return descriptors, nil
}

func protoSource(files FileSource, root, name string) (io.ReadCloser, error) {
	relative := filepath.FromSlash(name)
	if !filepath.IsLocal(relative) {
		return nil, fmt.Errorf("invalid proto import %q", name)
	}
	data, err := files.Read(filepath.Join(root, relative))
	if err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader(string(data))), nil
}

var errRPCDescriptorsTooLarge = errors.New("gRPC descriptors exceed the size limit")

func describeRPC(encoded string) (RPCSchema, error) {
	schema := RPCSchema{Descriptors: encoded, Methods: []RPCMethod{}}
	types, err := rpcDescriptors(encoded)
	if err != nil || types == nil {
		return schema, err
	}
	types.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		for i := 0; i < file.Services().Len(); i++ {
			service := file.Services().Get(i)
			for j := 0; j < service.Methods().Len(); j++ {
				method := service.Methods().Get(j)
				schema.Methods = append(schema.Methods, RPCMethod{
					Service: string(service.FullName()), Name: string(method.Name()),
					Input: string(method.Input().Name()), Output: string(method.Output().Name()),
					Example: rpcExample(method.Input()), Streaming: method.IsStreamingServer(),
					ClientStreaming: method.IsStreamingClient(),
				})
			}
		}
		return true
	})
	slices.SortFunc(schema.Methods, func(a, b RPCMethod) int {
		return strings.Compare(a.Service+"/"+a.Name, b.Service+"/"+b.Name)
	})
	return schema, nil
}

func rpcDescriptors(encoded string) (*rpcResolver, error) {
	if encoded == "" {
		return nil, nil
	}
	if len(encoded) > base64.StdEncoding.EncodedLen(domain.MaxBodyBytes) {
		return nil, errRPCDescriptorsTooLarge
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decoding gRPC descriptors: %w", err)
	}
	if len(data) > domain.MaxBodyBytes {
		return nil, errRPCDescriptorsTooLarge
	}
	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(data, &set); err != nil {
		return nil, fmt.Errorf("reading gRPC descriptors: %w", err)
	}
	return newRPCResolver(&set)
}

func rpcTypes(files []protoreflect.FileDescriptor) (*rpcResolver, error) {
	set, err := rpcDescriptorSet(files)
	if err != nil {
		return nil, err
	}
	return newRPCResolver(set)
}

func encodeRPCDescriptors(files []protoreflect.FileDescriptor) (string, error) {
	set, err := rpcDescriptorSet(files)
	if err != nil {
		return "", err
	}
	data, err := proto.Marshal(set)
	if err != nil {
		return "", err
	}
	if len(data) > domain.MaxBodyBytes {
		return "", errRPCDescriptorsTooLarge
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func rpcDescriptorSet(files []protoreflect.FileDescriptor) (*descriptorpb.FileDescriptorSet, error) {
	set := &descriptorpb.FileDescriptorSet{}
	seen := make(map[string]*descriptorpb.FileDescriptorProto)
	visited := make(map[protoreflect.FileDescriptor]bool)
	var collect func(protoreflect.FileDescriptor) error
	collect = func(file protoreflect.FileDescriptor) error {
		if file.IsPlaceholder() || visited[file] {
			return nil
		}
		visited[file] = true
		descriptor := protodesc.ToFileDescriptorProto(file)
		descriptor.SourceCodeInfo = nil
		previous, known := seen[file.Path()]
		if known && !proto.Equal(previous, descriptor) {
			return fmt.Errorf("conflicting definitions for proto file %q", file.Path())
		}
		seen[file.Path()] = descriptor
		for i := 0; i < file.Imports().Len(); i++ {
			if err := collect(file.Imports().Get(i).FileDescriptor); err != nil {
				return err
			}
		}
		if !known {
			set.File = append(set.File, descriptor)
		}
		return nil
	}
	for _, file := range files {
		if err := collect(file); err != nil {
			return nil, err
		}
	}
	return set, nil
}

func (s *Drafts) schemaInput(ctx context.Context, draft domain.Draft) (SendInput, error) {
	options := optionsOf(draft.Options)
	options.Variables, options.OperationName = "", ""
	draft.Options = &options
	draft.Body = ""
	above := s.above(ctx, draft.ID)
	return s.Prepared(ctx, draft, above.Auth, above.Variables)
}
