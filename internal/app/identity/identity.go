// Package identity carries the authenticated caller through a context. It is
// defined in the app layer so use cases and transports share one key without
// the app layer importing any infrastructure.
package identity

import "context"

type ctxKey struct{}

// WithAccountID returns a context carrying the authenticated account id.
func WithAccountID(ctx context.Context, accountID string) context.Context {
	return context.WithValue(ctx, ctxKey{}, accountID)
}

// AccountID returns the authenticated account id, if any.
func AccountID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxKey{}).(string)
	return id, ok && id != ""
}
