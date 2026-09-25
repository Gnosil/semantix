package boot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"semantix/harness/agent"
	"semantix/harness/agent/testutil"
	"semantix/harness/config"
	"semantix/harness/control"
	"semantix/harness/event"
	"semantix/harness/extension/dispatch"
	"semantix/harness/plugin"
	"semantix/harness/semantix"
	kernelevent "semantix/kernel/event"
	"semantix/kernel/slice"
)

// Reuses the lifecycle observation seam from commit 9c2bff92.
func observeSemantixBridgeCloses(opts *Options) *atomic.Int32 {
	closes := new(atomic.Int32)
	opts.closeSemantixBridge = func(bridge *semantix.Bridge) error {
		closes.Add(1)
		return bridge.Close()
	}
	return closes
}

func requireOpenPluginHost(t *testing.T, host *plugin.Host) {
	t.Helper()
	_, err := host.Add(context.Background(), plugin.Spec{Name: "lifecycle-probe", Type: "unsupported"})
	if err == nil || !strings.Contains(err.Error(), "unknown transport type") {
		t.Fatalf("shared host probe = %v, want open-host transport validation", err)
	}
}

func writeBootSemantixFixture(t *testing.T) string {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	const revision = "1111111111111111111111111111111111111111"
	writeFile(t, dir, ".git/HEAD", revision+"\n")
	writeFile(t, dir, "semantix-agent.toml", fmt.Sprintf(`
default_model = "test-model"
[agent]
system_prompt = "BASE"
[environment]
enabled = false
[semantix]
enabled = true
mode = "strict"
project_dir = %q
sessions_dir = %q
[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`, dir, filepath.Join(dir, "mirrors")))
	if err := os.MkdirAll(filepath.Join(dir, ".semantix"), 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := slice.NewFileStore(filepath.Join(dir, ".semantix", "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.(io.Closer).Close()
	items := []*slice.Slice{
		{ID: "ctx-strong", Type: slice.Context, Content: []byte("repair parser regression with traceback"), Meta: slice.SliceMeta{SourceSession: "source-1"}},
		{ID: "ctx-runner", Type: slice.Context, Content: []byte("repair unrelated logging conventions"), Meta: slice.SliceMeta{SourceSession: "source-2"}},
	}
	for i := 0; i < 10; i++ {
		items = append(items, &slice.Slice{ID: fmt.Sprintf("other-%d", i), Type: slice.Prompt, Content: []byte("unrelated deployment configuration")})
	}
	for _, item := range items {
		item.Scope = slice.Project
		item.Meta.Origin = slice.OriginUserCurated
		item.Meta.BaseCommit = revision
		if err := store.Put(item); err != nil {
			t.Fatal(err)
		}
	}
	registerBootTokenProfileTestProvider()
	return dir
}

func TestBootSemantixLivesUntilControllerClose(t *testing.T) {
	for _, mode := range []string{"Build", "BuildRuntime", "extension-runtime"} {
		t.Run(mode, func(t *testing.T) {
			dir := writeBootSemantixFixture(t)
			if mode == "extension-runtime" {
				installBootFakePlugin(t, config.SemantixHomeDir(), "observer", map[string]any{
					"intercepts": []string{"input.receive"},
				})
			}
			prov := testutil.NewMock("boot-memory", testutil.Turn{Text: "done"})
			setBootTokenProfileTestProvider(t, prov)
			var diagnostics atomic.Int32
			opts := Options{Stderr: io.Discard, Sink: event.FuncSink(func(ev event.Event) {
				if ev.Kind == event.KernelCache && ev.KernelCache != nil && ev.KernelCache.Retrieval != nil && ev.KernelCache.Retrieval.Injected {
					diagnostics.Add(1)
				}
			})}
			sharedHost := plugin.NewHost()
			defer sharedHost.Close()
			opts.SharedHost = sharedHost
			closes := observeSemantixBridgeCloses(&opts)
			closeBridge := opts.closeSemantixBridge
			var closedBridge *semantix.Bridge
			var liveTicks atomic.Int32
			opts.closeSemantixBridge = func(bridge *semantix.Bridge) error {
				closedBridge = bridge
				unsubscribe := bridge.Events().Subscribe(func(ev kernelevent.Event) {
					if ev.Kind == kernelevent.EvolutionTick {
						liveTicks.Add(1)
					}
				})
				defer unsubscribe()
				for i := 0; i < 60; i++ {
					bridge.Events().Emit(kernelevent.Event{Kind: kernelevent.PrefetchWaste})
				}
				return closeBridge(bridge)
			}
			var ctrl *control.Controller
			var res *BuildResult
			var err error
			if mode == "Build" {
				ctrl, err = Build(context.Background(), opts)
			} else {
				res, err = BuildRuntime(context.Background(), opts)
				if err == nil {
					ctrl = res.Controller
					if mode == "extension-runtime" && (res.Runtime.Len() == 0 || res.Dispatcher == nil) {
						t.Fatal("fixture did not build a live extension runtime")
					}
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(ctrl.Close)
			ctrl.SetSessionPath(agent.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
			if got := closes.Load(); got != 0 {
				t.Errorf("bridge closes after successful boot = %d, want 0", got)
			}
			if err := ctrl.Run(context.Background(), "repair parser regression"); err != nil {
				t.Fatal(err)
			}
			requests := prov.Requests()
			if len(requests) != 1 {
				t.Fatalf("provider requests = %d, want 1", len(requests))
			}
			injected := false
			for _, message := range requests[0].Messages {
				injected = injected || strings.Contains(message.Content, "[semantix-reuse]")
			}
			if !injected {
				t.Fatal("fixture did not reach actual controller-to-provider injection")
			}
			if diagnostics.Load() == 0 {
				t.Error("successful boot lost the live KernelCache retrieval diagnostic")
			}
			ctrl.Close()
			if res != nil && !res.Runtime.Closed() {
				t.Error("controller cleanup left the runtime set open")
			}
			if liveTicks.Load() == 0 {
				t.Error("evolution was not live until controller cleanup")
			}
			ctrl.Close()
			ctrl.ReleaseResources()
			if got := closes.Load(); got != 1 {
				t.Errorf("bridge closes after repeated controller cleanup = %d, want 1", got)
			}
			assertBootSemantixClosed(t, closedBridge)
			requireOpenPluginHost(t, sharedHost)
			// Controller cleanup must drain the asynchronous Injected write.
			store, err := slice.NewFileStore(filepath.Join(dir, ".semantix", "project.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.(io.Closer).Close()
			item, err := store.Get("ctx-strong")
			// Speculative warming can inject again; the lifecycle contract is that
			// real injection is persisted, not that this turn has one lookup.
			if err != nil || item == nil || item.Stats.Injected < 1 {
				t.Fatalf("persisted injection = %+v, %v; want Injected>0", item, err)
			}
			t.Logf("provider_requests=1 diagnostics=%d Injected=%d evolution_ticks=%d closes=%d", diagnostics.Load(), item.Stats.Injected, liveTicks.Load(), closes.Load())
		})
	}
}

func TestBootSemantixClosesOnBuildError(t *testing.T) {
	for _, stage := range []string{"before-controller", "after-controller"} {
		t.Run(stage, func(t *testing.T) {
			writeBootSemantixFixture(t)
			setBootTokenProfileTestProvider(t, testutil.NewMock("unused"))
			opts := Options{Sink: event.Discard, Stderr: io.Discard}
			closes := observeSemantixBridgeCloses(&opts)
			closeBridge := opts.closeSemantixBridge
			var closedBridge *semantix.Bridge
			opts.closeSemantixBridge = func(bridge *semantix.Bridge) error {
				closedBridge = bridge
				return closeBridge(bridge)
			}
			if stage == "before-controller" {
				opts.Model = "missing-model"
			} else {
				installBootFakePlugin(t, config.SemantixHomeDir(), "blocker", map[string]any{
					"replaces": []string{"system_prompt"},
					"env":      map[string]string{bootFakeEnvBlockEvent: "system_prompt.build"},
				})
			}
			res, err := BuildRuntime(context.Background(), opts)
			if err == nil || res != nil {
				t.Fatalf("build result = %v, %v; want failure", res, err)
			}
			var blocked *dispatch.BlockError
			if stage == "before-controller" && !errors.Is(err, ErrUnknownModel) || stage == "after-controller" && !errors.As(err, &blocked) {
				t.Fatalf("build failed at the wrong stage: %v", err)
			}
			if got := closes.Load(); got != 1 {
				t.Errorf("bridge closes after %s error = %d, want 1", stage, got)
			}
			assertBootSemantixClosed(t, closedBridge)
			t.Logf("%s closes=%d; admission, diagnostics, evolution closed", stage, closes.Load())
		})
	}
}

func assertBootSemantixClosed(t *testing.T, bridge *semantix.Bridge) {
	t.Helper()
	if bridge == nil {
		t.Fatal("boot did not construct its bridge")
	}
	// Read-only lifecycle inspection avoids adding public API just for tests.
	v := reflect.ValueOf(bridge).Elem()
	if !v.FieldByName("closing").Bool() || !v.FieldByName("sink").IsNil() || !v.FieldByName("evolution").Elem().FieldByName("unsub").IsNil() {
		t.Error("boot teardown left bridge admission, diagnostics, or evolution live")
	}
}
