package request

import (
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	reflectionpb "google.golang.org/grpc/reflection/grpc_reflection_v1"

	"schemo/internal/domain"
)

type listedServices map[string]grpc.ServiceInfo

func (s listedServices) GetServiceInfo() map[string]grpc.ServiceInfo { return s }

func serveRPC(t *testing.T, register func(*grpc.Server)) *grpc.ClientConn {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	register(server)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	client, err := grpc.NewClient("passthrough:///"+listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestReflectionSkipsAServiceTheServerCannotDescribe(t *testing.T) {
	client := serveRPC(t, func(server *grpc.Server) {
		healthpb.RegisterHealthServer(server, health.NewServer())
		reflectionpb.RegisterServerReflectionServer(server, reflection.NewServerV1(reflection.ServerOptions{
			Services: listedServices{"ghost.v1.Service": {}, "grpc.health.v1.Health": {}},
		}))
	})
	files, warnings, err := reflectServices(t.Context(), client)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Services().ByName("Health") == nil {
		t.Fatalf("files = %v", files)
	}
	if len(warnings) != 1 || !strings.HasPrefix(warnings[0], "ghost.v1.Service: ") {
		t.Fatalf("warnings = %q", warnings)
	}
}

func TestReflectionFailsWhenNoServiceResolves(t *testing.T) {
	client := serveRPC(t, func(server *grpc.Server) {
		reflectionpb.RegisterServerReflectionServer(server, reflection.NewServerV1(reflection.ServerOptions{
			Services: listedServices{"ghost.v1.Service": {}},
		}))
	})
	if _, _, err := reflectServices(t.Context(), client); err == nil || !strings.Contains(err.Error(), "ghost.v1.Service") {
		t.Fatalf("err = %v", err)
	}
}

// A Connect server without the reflection handler answers the reflection path with a plain 404.
func TestServerWithoutReflectionIsNamedAsSuch(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	server := &http.Server{Protocols: protocols, Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	client, err := grpc.NewClient("passthrough:///"+listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	if _, _, err := reflectServices(t.Context(), client); domain.CodeOf(err) != domain.CodeNoReflection {
		t.Fatalf("err = %v", err)
	}
}

func TestServerListingNoServicesIsAnError(t *testing.T) {
	client := serveRPC(t, func(server *grpc.Server) {
		reflectionpb.RegisterServerReflectionServer(server, reflection.NewServerV1(reflection.ServerOptions{
			Services: listedServices{},
		}))
	})
	if _, _, err := reflectServices(t.Context(), client); domain.CodeOf(err) != domain.CodeNoServices {
		t.Fatalf("err = %v", err)
	}
}

type silentReflection struct {
	reflectionpb.UnimplementedServerReflectionServer
}

func (silentReflection) ServerReflectionInfo(stream reflectionpb.ServerReflection_ServerReflectionInfoServer) error {
	<-stream.Context().Done()
	return stream.Context().Err()
}

// The request context has no deadline here, as with the request timeout turned off.
func TestSilentReflectionEndsAtItsOwnDeadline(t *testing.T) {
	previous := rpcReflectionTimeout
	rpcReflectionTimeout = 200 * time.Millisecond
	t.Cleanup(func() { rpcReflectionTimeout = previous })
	client := serveRPC(t, func(server *grpc.Server) {
		reflectionpb.RegisterServerReflectionServer(server, silentReflection{})
	})

	done := make(chan error, 2)
	go func() {
		_, _, err := reflectServices(t.Context(), client)
		done <- err
	}()
	go func() {
		_, err := reflectFile(t.Context(), client, "a.v1.Service")
		done <- err
	}()
	for range 2 {
		select {
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), "DeadlineExceeded") {
				t.Errorf("err = %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("reflection did not return")
		}
	}
}

func TestRPCHintNamesTheSchemeToUse(t *testing.T) {
	const notTLS = "connection error: tls: first record does not look like a TLS handshake"
	const wantsTLS = "connection error: error reading server preface: EOF"
	for _, row := range []struct{ message, address, want string }{
		{notTLS, "grpcs://api.test", "use grpc:// instead of grpcs://"},
		{notTLS, "https://api.test", "use http:// instead of https://"},
		{wantsTLS, "api.test:443", "use grpcs://"},
		{wantsTLS, "http://api.test", "use https://"},
		{wantsTLS, "grpcs://api.test", ""},
		{notTLS, "grpc://api.test", ""},
	} {
		got := rpcHint(row.message, row.address)
		if row.want == "" && got != row.message || row.want != "" && (!strings.Contains(got, row.want) || !strings.HasSuffix(got, row.message)) {
			t.Errorf("rpcHint(%q, %q) = %q", row.message, row.address, got)
		}
	}
}
