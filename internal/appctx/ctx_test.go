package appctx

import (
	"context"
	"testing"
)

func TestTxContextRoundtrip(t *testing.T) {
	type otherTx struct{}

	ctx := context.Background()

	if _, ok := TxFromContext[*otherTx](ctx); ok {
		t.Fatal("empty context reported an active transaction")
	}

	txCtx := WithTx(ctx, &otherTx{})

	if _, ok := TxFromContext[*struct{}](txCtx); ok {
		t.Error("wrong type matched the transaction value")
	}

	got, ok := TxFromContext[*otherTx](txCtx)
	if !ok || got == nil {
		t.Errorf("TxFromContext = (%v, %v), want stored value", got, ok)
	}
}
