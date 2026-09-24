package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/harness/event"
	"semantix/harness/extension"
	"semantix/harness/extension/dispatch"
	"semantix/harness/extension/protocol"
	"semantix/harness/provider"
	"semantix/harness/provider/anthropic"
	"semantix/harness/provider/openai"
	"semantix/harness/provider/responses"
	"semantix/harness/semantix"
	"semantix/harness/tool"
	"semantix/kernel/slice"
)

type rejectedMemoryProvider struct{}

func (rejectedMemoryProvider) Name() string { return "rejected" }
func (rejectedMemoryProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("request rejected")
}

func memoryDeliveryAgent(t *testing.T, p provider.Provider) (*Agent, *semantix.Bridge, string) {
	t.Helper()
	root := t.TempDir()
	commit := strings.Repeat("1", 40)
	for _, dir := range []string{".git", ".semantix"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte(commit), 0600); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(root, ".semantix", "project.db")
	s, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	card := &slice.Slice{ID: "parser-memory", Type: slice.Context, Scope: slice.Project, Content: []byte("parser regression parse parser"), Meta: slice.SliceMeta{Origin: slice.OriginSessionAuto, BaseCommit: commit, SourceSession: "prior"}}
	if err := s.Put(card); err != nil {
		t.Fatal(err)
	}
	if err := s.(interface{ Close() error }).Close(); err != nil {
		t.Fatal(err)
	}
	b := semantix.NewBridge(semantix.Config{Enabled: true, Mode: "strict", ProjectDir: root, SessionsDir: filepath.Join(root, "sessions"), Budget: 4096})
	t.Cleanup(func() { _ = b.Close() })
	sess := NewSession("system rules")
	sess.Messages = append(sess.Messages, provider.Message{Role: provider.RoleUser, Content: "repair parser regression"})
	a := New(p, tool.NewRegistry(), sess, Options{Semantix: b}, event.Discard)
	r := b.InjectDetailed(context.Background(), "repair parser regression")
	if r.Text == "" || len(r.Targets) != 1 {
		t.Fatalf("fixture failed: %+v", r)
	}
	a.turn.injectBlock = r.Text
	a.turn.injectTargets = r.Targets
	return a, b, db
}

func memoryDeliveryStats(t *testing.T, b *semantix.Bridge, db string) slice.SliceStats {
	t.Helper()
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer s.(interface{ Close() error }).Close()
	got, err := s.Get("parser-memory")
	if err != nil || got == nil {
		t.Fatalf("Get: %v %v", got, err)
	}
	return got.Stats
}

func TestMemoryDeliveryOnlyCreditsAcceptedFinalRequest(t *testing.T) {
	for _, kind := range []string{"prepared_only", "unused_prefetch", "removed", "blocked", "rewritten", "local_only", "cancelled", "provider_error", "budget_exit", "accepted_twice"} {
		t.Run(kind, func(t *testing.T) {
			var p provider.Provider = &mockProvider{name: "recorder", chunks: []provider.Chunk{{Type: provider.ChunkDone}}}
			if kind == "provider_error" {
				p = rejectedMemoryProvider{}
			}
			a, b, db := memoryDeliveryAgent(t, p)
			if kind == "unused_prefetch" {
				a.storePrefetch(&prefetchedInjectResult{Text: a.turn.injectBlock, Targets: a.turn.injectTargets, Turn: a.semantixTurn.Load()})
				a.wastePrefetch()
			}
			if kind == "removed" || kind == "rewritten" || kind == "blocked" {
				client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error) {
					if ev != protocol.EventProviderRequest {
						return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
					}
					if kind == "blocked" {
						return blockWith("stop"), nil
					}
					var in dispatch.ProviderRequestPayload
					if err := json.Unmarshal(payload, &in); err != nil {
						return protocol.InterceptResult{}, err
					}
					for i := range in.Request.Messages {
						if strings.Contains(in.Request.Messages[i].Content, "[semantix-reuse]") {
							in.Request.Messages[i].Content = "removed"
							if kind == "rewritten" {
								in.Request.Messages[i].Content = "[semantix-reuse]not the selected content[/semantix-reuse]"
							}
						}
					}
					return replaceWith(t, in), nil
				}}
				a.SetExtensions(newExtDispatcher(client, true, nil, extension.PointProviderRequest))
			}
			if kind == "budget_exit" {
				a.contextWindow = 1
				a.maxOutputTokens = 1
			}
			req, err := a.prepareSamplingRequest(context.Background())
			if kind == "blocked" || kind == "budget_exit" {
				if err == nil {
					t.Fatal("fixture did not block")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if kind == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if kind == "local_only" {
				for i := range req.req.Messages {
					if strings.Contains(req.req.Messages[i].Content, "[semantix-reuse]") {
						req.req.Messages[i].LocalOnly = true
					}
				}
			}
			if err == nil && kind != "prepared_only" && kind != "unused_prefetch" {
				ch, sendErr := a.streamProviderRequest(ctx, req.req)
				if kind == "provider_error" || kind == "cancelled" {
					if sendErr == nil {
						t.Error("send should fail")
					}
				} else if sendErr != nil {
					t.Fatal(sendErr)
				}
				if ch != nil {
					for range ch {
					}
				}
				if kind == "accepted_twice" {
					ch, err = a.streamProviderRequest(ctx, req.req)
					if err != nil {
						t.Fatal(err)
					}
					for range ch {
					}
				}
			}
			a.armLoopGuardPass(0)
			got := memoryDeliveryStats(t, b, db)
			want := uint64(0)
			if kind == "accepted_twice" {
				want = 1
			}
			if got.Injected != want || got.Rejected != want {
				t.Fatalf("%s stats=%+v want delivery/rejection=%d", kind, got, want)
			}
		})
	}
}

func drainMemoryRequest(t *testing.T, a *Agent, req provider.Request) {
	t.Helper()
	ch, err := a.streamProviderRequest(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
	}
}

func TestMemoryDeliveryHTTPSerializesUnprivilegedHistory(t *testing.T) {
	for _, kind := range []string{"openai", "anthropic", "responses"} {
		t.Run(kind, func(t *testing.T) {
			bodies := make(chan map[string]any, 4)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				bodies <- body
				w.Header().Set("Content-Type", "text/event-stream")
				switch kind {
				case "openai":
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				case "anthropic":
					fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"answer\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
				default:
					fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"answer\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp1\"}}\n\n")
				}
			}))
			defer srv.Close()
			cfg := provider.Config{Name: "local-recorder", BaseURL: srv.URL, Model: "fixture", APIKey: "fixture-not-a-real-key"}
			var p provider.Provider
			var err error
			switch kind {
			case "openai":
				p, err = openai.New(cfg)
			case "anthropic":
				p, err = anthropic.New(cfg)
			default:
				p = responses.New(responses.Config{Name: cfg.Name, BaseURL: srv.URL, Model: cfg.Model, APIKey: cfg.APIKey, Mode: "stateless"})
			}
			if err != nil {
				t.Fatal(err)
			}
			a, b, db := memoryDeliveryAgent(t, p)
			a.strictAlternatingRoles = true
			req, err := a.prepareSamplingRequest(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			drainMemoryRequest(t, a, req.req)
			body := <-bodies
			messages, _ := body["messages"].([]any)
			if kind == "responses" {
				messages, _ = body["input"].([]any)
			}
			found := false
			for _, raw := range messages {
				msg := raw.(map[string]any)
				content, _ := json.Marshal(msg["content"])
				if strings.Contains(string(content), "parser-memory") {
					if msg["role"] != "user" {
						t.Fatalf("history elevated: %v", msg)
					}
					found = true
				}
			}
			if !found {
				t.Fatalf("serialized request lacks history: %v", body)
			}
			for _, key := range []string{"system", "instructions"} {
				raw, _ := json.Marshal(body[key])
				if strings.Contains(string(raw), "parser-memory") {
					t.Fatalf("history in privileged %s", key)
				}
			}
			// Replaying the frozen request is not a second per-turn use observation.
			drainMemoryRequest(t, a, req.req)
			<-bodies
			if got := memoryDeliveryStats(t, b, db); got.Injected != 1 {
				t.Fatalf("accepted HTTP history stats=%+v", got)
			}
		})
	}
}

func TestMemoryDeliveryResponsesIncrementalAndExpiredReplay(t *testing.T) {
	bodies := make(chan map[string]any, 8)
	attempt := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		bodies <- body
		attempt++
		if attempt == 3 {
			http.Error(w, "previous_response_id expired", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"answer\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp%d\"}}\n\n", attempt)
	}))
	defer srv.Close()
	p := responses.New(responses.Config{Name: "local-stateful", BaseURL: srv.URL, Model: "fixture", APIKey: "fixture", Mode: "stateful"})
	a, b, db := memoryDeliveryAgent(t, p)
	frozen, err := a.prepareSamplingRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	req := frozen.req
	drainMemoryRequest(t, a, req)
	first := <-bodies
	raw, _ := json.Marshal(first["input"])
	if !strings.Contains(string(raw), "parser-memory") {
		t.Fatal("first wire request lacks history")
	}
	for _, prompt := range []string{"followup two", "followup three"} {
		req.Messages = append(req.Messages, provider.Message{Role: provider.RoleAssistant, Content: "answer"}, provider.Message{Role: provider.RoleUser, Content: prompt})
		drainMemoryRequest(t, a, req)
		wire := <-bodies
		if wire["previous_response_id"] == nil || wire["input"] != prompt {
			t.Fatalf("not incremental: %v", wire)
		}
		if prompt == "followup three" {
			wire = <-bodies
			raw, _ = json.Marshal(wire["input"])
			if wire["previous_response_id"] != nil || !strings.Contains(string(raw), "parser-memory") {
				t.Fatalf("expired ID lost full history: %v", wire)
			}
		}
	}
	// Removing history invalidates the stateful prefix, rather than silently
	// keeping the previous response's hidden history through its ID.
	var clean []provider.Message
	for _, msg := range req.Messages {
		if !strings.Contains(msg.Content, "parser-memory") {
			clean = append(clean, msg)
		}
	}
	req.Messages = clean
	drainMemoryRequest(t, a, req)
	wire := <-bodies
	raw, _ = json.Marshal(wire["input"])
	if wire["previous_response_id"] != nil || strings.Contains(string(raw), "parser-memory") {
		t.Fatalf("deleted history still inherited: %v", wire)
	}
	if got := memoryDeliveryStats(t, b, db); got.Injected != 1 {
		t.Fatalf("incremental/retry inflated use: %+v", got)
	}
}
