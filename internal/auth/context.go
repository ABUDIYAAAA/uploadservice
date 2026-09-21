package auth

import "context"

type contextKey struct{}

var identityKey = contextKey{}

// Identity represents the authenticated calling service identity.
type Identity struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ClientID     string `json:"client_id"`
	ServiceID    string `json:"service_id"`
	IsActive     bool   `json:"is_active"`
	TokenVersion int    `json:"token_version"`
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
