package ippool

import "context"

type exitKey struct{}

// WithExit receives the selected request endpoint before transport starts.
func WithExit(ctx context.Context, capture func(string)) context.Context {
	return context.WithValue(ctx, exitKey{}, capture)
}
func captureExit(ctx context.Context, exit string) {
	if f, ok := ctx.Value(exitKey{}).(func(string)); ok {
		f(exit)
	}
}
