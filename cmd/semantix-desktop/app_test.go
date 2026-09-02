package main

import (
	"context"
	"net"
	"testing"
	"time"

	"semantix/harness/boot"
	"semantix/harness/control"
)

func TestDesktopRuntimeIsLoopbackAndReleasesListener(t *testing.T) {
	t.Setenv("SEMANTIX_HOME", t.TempDir())
	original := buildDesktopController
	buildDesktopController = func(_ context.Context, opts boot.Options) (*control.Controller, error) {
		return control.New(control.Options{Sink: opts.Sink, WorkspaceRoot: opts.WorkspaceRoot, SessionDir: t.TempDir(), Label: "test"}), nil
	}
	t.Cleanup(func() { buildDesktopController = original })

	run, address, _, err := startDesktopRuntime(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		t.Fatalf("desktop address = %q, want loopback", address)
	}
	run.cancel()
	select {
	case err := <-run.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("desktop server did not stop")
	}
	run.controller.Close()
	run.leases.Release()

	listener, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatalf("desktop listener was not released: %v", err)
	}
	_ = listener.Close()
}
