package request

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"schemo/internal/domain"
)

func rpcReplayOptions(options domain.ProtocolOptions) *domain.ReplayOptions {
	return &domain.ReplayOptions{RPCMethod: options.RPCMethod, Descriptors: options.Descriptors}
}

func rpcRecord(body string, in SendInput) (string, *domain.ReplayOptions, string) {
	if in.RecordOptions == nil {
		return body, nil, ""
	}
	return body, in.RecordOptions, strings.TrimPrefix(in.RecordOptions.RPCMethod, "/")
}

func rpcClient(raw string, cfg EngineConfig) (*grpc.ClientConn, error) {
	address := strings.TrimSpace(raw)
	if !strings.Contains(address, "://") {
		address = "grpc://" + address
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" ||
		parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, fmt.Errorf("gRPC address must be a host and port")
	}
	port := parsed.Port()
	creds := insecure.NewCredentials()
	var options []grpc.DialOption
	switch parsed.Scheme {
	case "grpc", "http":
		if port == "" {
			port = "80"
		}
	case "grpcs", "https":
		if port == "" {
			port = "443"
		}
		if cfg.TrustLocalhost && domain.LocalName(parsed.Hostname()) && !cfg.SkipTLSVerify {
			cfg.SkipTLSVerify = true
			options = append(options, grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
				return loopbackDial(ctx, "tcp", addr)
			}))
		}
		tlsConfig := tlsConfigFor(cfg)
		if tlsConfig == nil {
			tlsConfig = &tls.Config{}
		}
		creds = credentials.NewTLS(tlsConfig)
	default:
		return nil, fmt.Errorf("unsupported gRPC scheme %q", parsed.Scheme)
	}
	// A custom dialer owns routing for the localhost exception and SOCKS.
	if len(options) != 0 || cfg.Proxy.Mode == domain.ProxyOff ||
		(cfg.Proxy.Mode == domain.ProxyManual && cfg.Proxy.Address() == "") {
		options = append(options, grpc.WithNoProxy())
	} else if cfg.Proxy.Mode == domain.ProxyManual && cfg.Proxy.Protocol == domain.ProxySOCKS5 {
		dial := socksDial(cfg.Proxy)
		options = append(options, grpc.WithNoProxy(), grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return dial(ctx, "tcp", addr)
		}))
	} else if cfg.Proxy.Mode == domain.ProxyManual {
		target := &http.Request{URL: &url.URL{Scheme: "https", Host: net.JoinHostPort(parsed.Hostname(), port)}}
		proxy, err := proxyOf(cfg.Proxy)(target)
		if err != nil {
			return nil, err
		}
		if proxy != nil {
			return nil, fmt.Errorf("gRPC supports direct connections, the system proxy and SOCKS5")
		}
		options = append(options, grpc.WithNoProxy())
	}
	target := "passthrough:///" + net.JoinHostPort(parsed.Hostname(), port)
	return grpc.NewClient(target, append(options, grpc.WithTransportCredentials(creds))...)
}

func rpcContext(ctx context.Context, headers []domain.HeaderPair) (context.Context, error) {
	out := metadata.MD{}
	for _, pair := range headers {
		name := strings.ToLower(strings.TrimSpace(pair.Name))
		value := pair.Value
		if strings.HasSuffix(name, "-bin") {
			bytes, err := base64.StdEncoding.DecodeString(value)
			if err != nil {
				bytes, err = base64.RawStdEncoding.DecodeString(value)
			}
			if err != nil {
				return nil, fmt.Errorf("gRPC metadata %q requires Base64: %w", name, err)
			}
			value = string(bytes)
		}
		out.Append(name, value)
	}
	return metadata.NewOutgoingContext(ctx, out), nil
}

func rpcHeaders(headers, trailers metadata.MD) []domain.HeaderPair {
	var out []domain.HeaderPair
	for _, rows := range []metadata.MD{headers, trailers} {
		for _, name := range slices.Sorted(maps.Keys(rows)) {
			for _, value := range rows[name] {
				if strings.HasSuffix(name, "-bin") {
					value = base64.StdEncoding.EncodeToString([]byte(value))
				}
				out = append(out, domain.HeaderPair{Name: name, Value: value})
			}
		}
	}
	return out
}

type rpcCall struct {
	method      string
	descriptors string
}

func (e *Engine) callRPC(ctx context.Context, spec Spec, cfg EngineConfig, started time.Time) *domain.Response {
	response := &domain.Response{ContentType: domain.MediaJSON}
	defer func() { response.DurationUs = time.Since(started).Microseconds() }()

	service, method, ok := strings.Cut(strings.TrimPrefix(strings.TrimSpace(spec.rpc.method), "/"), "/")
	if !ok || !protoreflect.FullName(service).IsValid() || !protoreflect.Name(method).IsValid() {
		response.Error = "choose a gRPC method in service/Method form"
		return response
	}
	ctx, err := rpcContext(ctx, spec.Headers)
	if err != nil {
		return stopped(response, err)
	}
	client, err := rpcClient(spec.URL, cfg)
	if err != nil {
		return stopped(response, err)
	}
	defer client.Close()

	var file protoreflect.FileDescriptor
	resolver, err := rpcDescriptors(spec.rpc.descriptors)
	if err != nil {
		return stopped(response, err)
	}
	if resolver != nil {
		descriptor, err := resolver.FindDescriptorByName(protoreflect.FullName(service))
		if err != nil {
			return stopped(response, fmt.Errorf("gRPC service %s: %w", service, err))
		}
		file = descriptor.ParentFile()
	} else {
		file, err = reflectFile(ctx, client, protoreflect.FullName(service))
		if err != nil {
			return stopped(response, withRPCHint(fmt.Errorf("gRPC reflection: %w", err), spec.URL))
		}
		resolver, err = rpcTypes([]protoreflect.FileDescriptor{file})
		if err != nil {
			return stopped(response, err)
		}
	}
	svc := file.Services().ByName(protoreflect.FullName(service).Name())
	if svc == nil || string(svc.FullName()) != service {
		return stopped(response, fmt.Errorf("gRPC service %s was not found", service))
	}
	descriptor := svc.Methods().ByName(protoreflect.Name(method))
	if descriptor == nil {
		return stopped(response, fmt.Errorf("gRPC method %s/%s was not found", service, method))
	}
	if descriptor.IsStreamingClient() {
		return stopped(response, fmt.Errorf("gRPC method %s requires client streaming", method))
	}

	input := dynamicpb.NewMessage(descriptor.Input())
	body := strings.TrimSpace(spec.Body)
	if body == "" {
		body = "{}"
	}
	if err := (protojson.UnmarshalOptions{Resolver: resolver}).Unmarshal([]byte(body), input); err != nil {
		return stopped(response, fmt.Errorf("gRPC request JSON: %w", err))
	}
	output := dynamicpb.NewMessage(descriptor.Output())
	path := "/" + service + "/" + method
	limit := int(e.cfg.MaxBodyBytes)
	var headers, trailers metadata.MD
	jsonWriter := protojson.MarshalOptions{Resolver: resolver}
	if descriptor.IsStreamingServer() {
		response.Body, response.BodyTruncated, headers, trailers, err = receiveRPC(ctx, client, path, input, output, jsonWriter, limit)
	} else {
		err = client.Invoke(ctx, path, input, output, grpc.Header(&headers), grpc.Trailer(&trailers),
			grpc.MaxCallRecvMsgSize(limit))
		if err == nil {
			data, encodeErr := jsonWriter.Marshal(output)
			if encodeErr != nil {
				return stopped(response, encodeErr)
			}
			if len(data) > limit {
				response.BodyTruncated = true
				data = data[:limit]
			}
			response.Body = string(data)
		}
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return stopped(response, ctx.Err())
	}
	code := status.Code(err)
	response.Status, response.StatusText = int(code), code.String()
	response.Headers = rpcHeaders(headers, trailers)
	if err != nil {
		response.Error = rpcHint(status.Convert(err).Message(), spec.URL)
	}
	return response
}

func receiveRPC(
	ctx context.Context, client *grpc.ClientConn, path string, input, output *dynamicpb.Message,
	writer protojson.MarshalOptions, limit int,
) (body string, truncated bool, headers, trailers metadata.MD, err error) {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := client.NewStream(streamCtx, &grpc.StreamDesc{ServerStreams: true}, path, grpc.MaxCallRecvMsgSize(limit))
	if err != nil {
		return "", false, nil, nil, err
	}
	if err = stream.SendMsg(input); err != nil && !errors.Is(err, io.EOF) {
		return "", false, nil, stream.Trailer(), err
	}
	if err = stream.CloseSend(); err != nil {
		return "", false, nil, stream.Trailer(), err
	}
	var data strings.Builder
	data.WriteByte('[')
	for {
		output.Reset()
		err = stream.RecvMsg(output)
		if err != nil {
			break
		}
		encoded, encodeErr := writer.Marshal(output)
		if encodeErr != nil {
			err = status.Error(codes.Internal, encodeErr.Error())
			break
		}
		if data.Len()+len(encoded)+2 > limit {
			truncated = true
			break
		}
		if data.Len() > 1 {
			data.WriteByte(',')
		}
		data.Write(encoded)
	}
	data.WriteByte(']')
	if errors.Is(err, io.EOF) {
		err = nil
	}
	headers, _ = stream.Header()
	return data.String(), truncated, headers, stream.Trailer(), err
}
