package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStartupDrainCoordinatorDrainsWhenCanceledBeforeRegistration(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	startup := newStartupDrainCoordinator()
	drained := make(chan struct{})
	go startup.drainOnCancel(ctx, func() {
		close(drained)
	})

	// Model main passing its pre-start cancellation check, followed by a signal
	// arriving before zrpc invokes the registration callback.
	require.NoError(t, ctx.Err())
	cancel()
	startup.registered(ctx, func() {
		t.Fatal("canceled server must not become ready")
	})

	select {
	case <-startup.done:
	case <-time.After(time.Second):
		t.Fatal("drain remained blocked waiting for startup result")
	}
	select {
	case <-drained:
	default:
		t.Fatal("server registration completed without draining")
	}
}

func TestStartupDrainCoordinatorSkipsDrainWhenStartupIsAborted(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	startup := newStartupDrainCoordinator()
	drained := make(chan struct{}, 1)
	go startup.drainOnCancel(ctx, func() {
		drained <- struct{}{}
	})

	cancel()
	startup.abort()

	select {
	case <-startup.done:
	case <-time.After(time.Second):
		t.Fatal("abort remained blocked waiting for startup result")
	}
	select {
	case <-drained:
		t.Fatal("startup aborted before registration must not drain")
	default:
	}
}
