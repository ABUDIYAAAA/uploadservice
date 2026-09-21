package auth

import "context"

type contextKey struct{}

var identityKey = contextKey{}

// Identity represents the authenticated service identity.
type Identity struct {
	ServiceID string `json:"service_id"`
}

// WithIdentity attaches the verified identity to the context.
func WithIdentity(ctx context.Context, identity *Identity) context.Context {
	return context.WithValue(ctx, identityKey, identity)
}

// IdentityFromContext extracts the verified identity from the context.
func IdentityFromContext(ctx context.Context) (*Identity, bool) {
	identity, ok := ctx.Value(identityKey).(*Identity)
	return identity, ok && identity != nil
}
