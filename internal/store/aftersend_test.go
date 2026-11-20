package store

import (
	"context"
	"testing"
	"time"
)

func TestAfterSendSurvivesCancellationButIsBounded(t *testing.T) {
	t.Parallel()
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	ctx, done := AfterSend(parent)
	defer done()
	if err := ctx.Err(); err != nil {
		t.Fatalf("AfterSend context cancelled with its parent: %v", err)
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > AfterSendTimeout || time.Until(deadline) <= 0 {
		t.Errorf("deadline = %v (ok=%v), want within %v", deadline, ok, AfterSendTimeout)
	}
}
