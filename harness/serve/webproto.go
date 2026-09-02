package serve

// GUI-4 (#407): the stable frontend event protocol. The existing /events
// stream keeps emitting raw eventwire JSON for the console and desktop
// clients; /workspace/events wraps THE SAME underlying flow in a versioned
// envelope with canonical types, broadcaster-lifetime sequence numbers and a task id,
// so browser surfaces can consume, validate and resume it without a second
// data model.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"semantix/harness/event"
	"semantix/harness/eventwire"
)

// Canonical frontend event types (issue #407). diff and plan are part of the
// contract but RESERVED: no internal event carries them yet, and fabricating
// frames is forbidden by the acceptance criteria — they appear once a real
// producer exists.
const (
	ProtoTypeUser              = "user_message"
	ProtoTypeAssistant         = "assistant_message"
	ProtoTypePlan              = "plan" // reserved, never emitted in v1
	ProtoTypeToolStart         = "tool_start"
	ProtoTypeToolResult        = "tool_result"
	ProtoTypeDiff              = "diff" // reserved, never emitted in v1
	ProtoTypePermissionRequest = "permission_request"
	ProtoTypeTaskStatus        = "task_status"
	ProtoTypeCacheStatus       = "cache_status"
	ProtoTypeError             = "error"

	// ProtoTypeUnknown wraps kinds added after this protocol version. Clients
	// MUST ignore types they do not recognize instead of failing.
	ProtoTypeUnknown = "unknown"
)

var protoTypes = map[string]bool{
	ProtoTypeUser: true, ProtoTypeAssistant: true, ProtoTypePlan: true,
	ProtoTypeToolStart: true, ProtoTypeToolResult: true, ProtoTypeDiff: true,
	ProtoTypePermissionRequest: true, ProtoTypeTaskStatus: true,
	ProtoTypeCacheStatus: true, ProtoTypeError: true, ProtoTypeUnknown: true,
}

// protoSkips lists wire kinds that are host-internal or renderer noise and are
// deliberately NOT forwarded to workspace clients (filtered, not mapped):
// stream_attempt is host-local sampling lifecycle, extension_surface needs an
// extension-aware renderer first, mcp_surface_ready / workspace_changed /
// context_maintenance are console-refresh hints already reflected server-side.
var protoSkips = map[string]bool{
	"stream_attempt":      true,
	"extension_surface":   true,
	"mcp_surface_ready":   true,
	"workspace_changed":   true,
	"context_maintenance": true,
}

// wireKindKnown tracks every kind eventwire can emit, so forward-compat
// classification can distinguish "known but skipped/typed" from genuinely new
// kinds without duplicating eventwire's registry in this package.
var wireKindKnown = func() map[string]bool {
	known := make(map[string]bool)
	for _, name := range eventwire.KindNames() {
		known[name] = true
	}
	return known
}()

// ProtoTypeFor maps a wire kind to its canonical frontend type. The second
// return reports whether the kind is forwarded at all (skipped kinds never
// reach the wire). Unknown kinds map to ProtoTypeUnknown so newly added
// backend events degrade gracefully on old clients instead of crashing them.
//
// turn_done defaults here to task_status; classifyFrame promotes it to error
// when its Err payload is set — the ONLY real failure signal in the stream
// (Notice carries no error Level, only info/warn).
func ProtoTypeFor(wireKind string) (string, bool) {
	if protoSkips[wireKind] {
		return "", false
	}
	switch wireKind {
	case "steer":
		return ProtoTypeUser, true
	case "text", "message", "reasoning":
		return ProtoTypeAssistant, true
	case "tool_dispatch", "tool_progress":
		return ProtoTypeToolStart, true
	case "tool_result":
		return ProtoTypeToolResult, true
	case "approval_request", "ask_request":
		return ProtoTypePermissionRequest, true
	case "usage", "compaction_started", "compaction_done":
		// Usage carries session cache hit/miss tokens; compaction reshapes the
		// cacheable context — both are truthful cache-status inputs.
		return ProtoTypeCacheStatus, true
	case "kernel_cache":
		// Kernel cache observations share the cache-status renderer with Usage.
		return ProtoTypeCacheStatus, true
	default:
		if !wireKindKnown[wireKind] {
			return ProtoTypeUnknown, true
		}
		return ProtoTypeTaskStatus, true
	}
}

// frameProbe decodes just enough of a wire frame to classify it.
type frameProbe struct {
	Kind  string `json:"kind"`
	Level string `json:"level,omitempty"`
	Err   string `json:"err,omitempty"`
}

func classifyFrame(frame []byte) (protoType string, keep bool, probe frameProbe) {
	if err := json.Unmarshal(frame, &probe); err != nil {
		return "", false, probe // tolerate junk rather than kill the stream
	}
	protoType, keep = ProtoTypeFor(probe.Kind)
	if !keep {
		return "", false, probe
	}
	// turn_done is the stream's only failure signal: a non-empty Err promotes
	// it from task_status to error. Notices never promote — they only carry
	// info/warn levels.
	if probe.Kind == "turn_done" && strings.TrimSpace(probe.Err) != "" {
		protoType = ProtoTypeError
	}
	return protoType, true, probe
}

// WebEnvelope is one SSE frame of GET /workspace/events. Data embeds the
// untouched eventwire frame — the single source of truth, so the protocol can
// never fabricate tool results or cache numbers (#407 acceptance).
type WebEnvelope struct {
	V      int             `json:"v"`
	Seq    uint64          `json:"seq"`
	Type   string          `json:"type"`
	TaskID string          `json:"task_id"`
	TimeMS int64           `json:"time_ms"`
	Data   json.RawMessage `json:"data"`
}

// workspaceEvents streams GET /workspace/events as SSE envelopes. A connection
// is bound to ONE task: task_id snapshots the active session path at subscribe
// time, so consumers can validate ordering and attribution per task, and
// reconnect around task switches with a fresh snapshot.
//
// Frame form (full SSE, ahead of the bare data:-only /events stream):
//
//	retry: 3000            reconnect cadence hint
//	id: <seq>              monotonic, gap-detectable
//	event: <canonical type>
//	data: {"v":1,...,"data":<untouched wire frame>}
//
// Unknown event types still arrive as valid SSE — EventSource dispatches
// unrecognized names to consumers that registered for them and drops the rest;
// clients MUST treat unregistered types as no-op rather than throwing
// (acceptance: 未知事件不会导致页面崩溃).
func (s *Server) workspaceEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	taskID := filepath.Base(s.ctl().SessionPath())
	if taskID == "" || taskID == "." || taskID == string(os.PathSeparator) {
		taskID = "current"
	}
	lastSeq := parseLastEventID(r.Header.Get("Last-Event-ID"))
	var ch <-chan webDelivery
	var replay []webDelivery
	var gap bool
	var unsubscribe func()
	if lastSeq == 0 && r.URL.Query().Get("live") == "1" {
		ch, unsubscribe = s.bc.SubscribeWebLive(taskID)
	} else {
		ch, replay, gap, unsubscribe = s.bc.SubscribeWebSince(lastSeq, taskID)
	}
	defer unsubscribe()
	if gap {
		w.Header().Set("X-Semantix-Replay", "gap")
	} else if lastSeq > 0 {
		w.Header().Set("X-Semantix-Replay", "complete")
	} else {
		w.Header().Set("X-Semantix-Replay", "initial")
	}

	fmt.Fprint(w, ": connected\n\nretry: 3000\n\n")
	flusher.Flush()
	if gap {
		// This is a transport comment, not a fabricated protocol event. The
		// client must refresh /history because the missing prefix is outside
		// the bounded replay window.
		fmt.Fprint(w, ": replay-gap; refresh /history\n\n")
		flusher.Flush()
	}
	for _, delivery := range replay {
		writeWebDelivery(w, flusher, delivery)
	}
	if lastSeq == 0 || gap {
		// Initial clients need actionable prompts that may have been emitted
		// before they connected. Reconnects receive those events from history
		// when available; a replay gap has no trustworthy history, so prompts
		// are safely re-emitted for the new connection.
		s.ctl().ReplayPendingPromptsWith(func() event.Sink {
			return event.FuncSink(func(e event.Event) {
				s.bc.EmitWebTo(ch, taskID, e)
			})
		})
	}

	keepalive := time.NewTicker(sseKeepaliveInterval)
	defer keepalive.Stop()

	for {
		select {
		case delivery, ok := <-ch:
			if !ok {
				return
			}
			writeWebDelivery(w, flusher, delivery)
		case <-keepalive.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func parseLastEventID(value string) uint64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	seq, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0
	}
	return seq
}

func writeWebDelivery(w http.ResponseWriter, flusher http.Flusher, delivery webDelivery) {
	payload, err := json.Marshal(WebEnvelope{
		V:      1,
		Seq:    delivery.frame.seq,
		Type:   delivery.frame.type_,
		TaskID: delivery.taskID,
		TimeMS: delivery.frame.timeMS,
		Data:   delivery.frame.data,
	})
	if err != nil {
		return
	}
	fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", delivery.frame.seq, delivery.frame.type_, payload)
	flusher.Flush()
}
