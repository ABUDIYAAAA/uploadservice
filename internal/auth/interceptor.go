package auth

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TokenVerifier defines the interface for token verification.
type TokenVerifier interface {
	Verify(ctx context.Context, token string) (*Identity, error)
}

// Interceptor provides gRPC interceptors for service token authentication.
type Interceptor struct {
	verifier TokenVerifier
}

// NewInterceptor creates a new authentication interceptor with the given verifier.
func NewInterceptor(verifier TokenVerifier) *Interceptor {
	return &Interceptor{
		verifier: verifier,
	}
}

// Unary intercepts unary gRPC requests and performs authentication.
func (i *Interceptor) Unary(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	identity, err := i.authenticate(ctx)
	if err != nil {
		return nil, err
	}

	ctx = WithIdentity(ctx, identity)
	return handler(ctx, req)
}

// Stream intercepts streaming gRPC requests and performs authentication once before invoking the handler.
func (i *Interceptor) Stream(
	srv any,
	stream grpc.ServerStream,
	info *grpc.StreamServerInfo,
	handler grpc.StreamHandler,
) error {
	identity, err := i.authenticate(stream.Context())
	if err != nil {
		return err
	}

	ctx := WithIdentity(stream.Context(), identity)
	wrapped := &authenticatedStream{
		ServerStream: stream,
		ctx:          ctx,
	}

	return handler(srv, wrapped)
}

func (i *Interceptor) authenticate(ctx context.Context) (*Identity, error) {
	token, err := extractBearerToken(ctx)
	if err != nil {
		return nil, err
	}

	identity, err := i.verifier.Verify(ctx, token)
	if err != nil {
		return nil, status.Error(
			codes.Unauthenticated,
			"invalid service token",
		)
	}

	return identity, nil
}

func extractBearerToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "missing metadata")
	}

	values := md.Get("authorization")
	if len(values) == 0 {
		return "", status.Error(codes.Unauthenticated, "missing authorization header")
	}

	val := strings.TrimSpace(values[0])
	if val == "" {
		return "", status.Error(codes.Unauthenticated, "empty authorization header")
	}

	parts := strings.SplitN(val, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return "", status.Error(codes.Unauthenticated, "invalid authorization scheme")
	}

	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", status.Error(codes.Unauthenticated, "empty token")
	}

	return token, nil
}

type authenticatedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *authenticatedStream) Context() context.Context {
	return s.ctx
}
