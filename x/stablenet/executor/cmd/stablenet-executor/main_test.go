package main

import (
	"context"
	"errors"
	"testing"
)

func TestFinishErrorPreservesFailureAndAcceptsJoinedCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := finishError(ctx, errors.Join(context.Canceled, nil)); err != nil {
		t.Fatalf("normal shutdown: %v", err)
	}
	want := errors.New("spool failure")
	if err := finishError(ctx, want); !errors.Is(err, want) {
		t.Fatalf("failure hidden: %v", err)
	}
	if err := finishError(ctx, errors.Join(context.Canceled, want)); !errors.Is(err, want) {
		t.Fatalf("cleanup failure hidden by cancellation: %v", err)
	}
	if err := finishError(context.Background(), context.Canceled); !errors.Is(err, context.Canceled) {
		t.Fatal("unrequested cancellation hidden")
	}
}
