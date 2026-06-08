package appctx

import "context"

type txKey struct{}

func WithTx(ctx context.Context, tx any) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

func TxFromContext[T any](ctx context.Context) (T, bool) {
	tx, ok := ctx.Value(txKey{}).(T)
	return tx, ok
}
