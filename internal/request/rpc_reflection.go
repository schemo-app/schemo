package request

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jhump/protoreflect/v2/grpcreflect"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"

	"schemo/internal/domain"
)

// Reflection lookups carry their own deadline: with the request timeout turned off, a server that
// keeps the stream open without answering would otherwise hold the call forever.
var rpcReflectionTimeout = 10 * time.Second

const rpcReflectionConcurrency = 8
const rpcMaxReflectedServices = 1000

// reflectionClient opens a reflection stream that ends with its context. The stream is cancelled,
// never Reset: Reset drains it and blocks for as long as the server stays silent.
func reflectionClient(ctx context.Context, client *grpc.ClientConn) (*grpcreflect.Client, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, rpcReflectionTimeout)
	return grpcreflect.NewClientAuto(ctx, client), cancel
}

func reflectFile(ctx context.Context, client *grpc.ClientConn, symbol protoreflect.FullName) (protoreflect.FileDescriptor, error) {
	reflection, cancel := reflectionClient(ctx, client)
	defer cancel()
	return reflection.FileContainingSymbol(symbol)
}

// reflectServices resolves every service on a stream of its own, so a service the server cannot
// describe, or describes too slowly, becomes a warning instead of failing the whole schema.
func reflectServices(ctx context.Context, client *grpc.ClientConn) ([]protoreflect.FileDescriptor, []string, error) {
	reflection, cancel := reflectionClient(ctx, client)
	listed, err := reflection.ListServices()
	cancel()
	if status.Code(err) == codes.Unimplemented {
		return nil, nil, domain.Refuse(domain.CodeNoReflection, domain.ErrNotAllowed, nil)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("gRPC reflection: %w", err)
	}
	var names []protoreflect.FullName
	for _, name := range listed {
		if !strings.HasPrefix(string(name), "grpc.reflection.") {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, nil, domain.Refuse(domain.CodeNoServices, domain.ErrNotFound, nil)
	}
	if len(names) > rpcMaxReflectedServices {
		return nil, nil, fmt.Errorf("gRPC reflection lists more than %d services", rpcMaxReflectedServices)
	}

	files := make([]protoreflect.FileDescriptor, len(names))
	failures := make([]error, len(names))
	var group errgroup.Group
	group.SetLimit(rpcReflectionConcurrency)
	for i, name := range names {
		if ctx.Err() != nil {
			break
		}
		group.Go(func() error {
			files[i], failures[i] = reflectFile(ctx, client, name)
			return nil
		})
	}
	_ = group.Wait() // Per-service failures are collected alongside the descriptors.
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	var resolved []protoreflect.FileDescriptor
	var warnings []string
	for i, name := range names {
		if failures[i] != nil {
			warnings = append(warnings, string(name)+": "+reflectionFailure(failures[i]))
			continue
		}
		resolved = append(resolved, files[i])
	}
	if len(resolved) == 0 {
		return nil, nil, fmt.Errorf("gRPC reflection: %s: %w", names[0], failures[0])
	}
	return resolved, warnings, nil
}

func reflectionFailure(err error) string {
	if status.Code(err) == codes.DeadlineExceeded {
		return "timed out"
	}
	return status.Convert(err).Message()
}

// rpcHint names the likely cause when the address and the server disagree about TLS: gRPC itself
// reports only a broken handshake or preface.
func rpcHint(message, address string) string {
	scheme, _, typed := strings.Cut(strings.ToLower(strings.TrimSpace(address)), "://")
	if !typed {
		scheme = "grpc"
	}
	switch {
	case (scheme == "grpcs" || scheme == "https") && strings.Contains(message, "first record does not look like a TLS handshake"):
		return fmt.Sprintf("The server may not use TLS, use %s:// instead of %s://. %s", strings.TrimSuffix(scheme, "s"), scheme, message)
	case (scheme == "grpc" || scheme == "http") && strings.Contains(message, "error reading server preface"):
		return fmt.Sprintf("The server may require TLS, use %ss://. %s", scheme, message)
	}
	return message
}

// withRPCHint keeps the original error in the chain, so a cancellation is still recognised.
func withRPCHint(err error, address string) error {
	if hinted := rpcHint(err.Error(), address); hinted != err.Error() {
		return fmt.Errorf("%s%w", strings.TrimSuffix(hinted, err.Error()), err)
	}
	return err
}
