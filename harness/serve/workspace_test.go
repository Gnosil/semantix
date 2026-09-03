package serve

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"semantix/harness/config"
	"semantix/harness/control"
)

// newWorkspaceTestServer builds a bare server like TestServeIndexPage does;
// the workspace shell renders without any controller interaction.
func newWorkspaceTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	srv := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func TestServeWorkspaceShellRenders(t *testing.T) {
	srv := newWorkspaceTestServer(t)

	resp, err := http.Get(srv.URL + "/workspace")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /workspace status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("GET /workspace content-type = %q, want text/html", ct)
	}

	htmlBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)

	for _, want := range []string{
		`data-ws-shell`,
		`data-ws-brand`,             // shared Semantix wordmark
		`/assets/logo-wordmark.svg`, // shared asset is the single branding source
		`id="ws-side"`,              // left nav rail
		`data-ws-side-toggle`,       // mobile drawer trigger
		`data-ws-side-close`,        // mobile drawer scrim
		`id="ws-context"`,           // right context panel
		`data-ws-collapse`,          // collapse control
		`aria-expanded="true"`,      // expanded by default at desktop widths
		`提出后续修改要求`,                  // composer placeholder
		`开始一个真实任务`,                  // empty-session start state
		`data-ws-context-diff`,      // real event-backed context surface
		`data-ws-cache-status`,      // GUI-9 cache observability hook
	} {
		if !strings.Contains(html, want) {
			t.Errorf("workspace shell missing %q", want)
		}
	}
}

func TestServeWorkspaceCacheStatusContract(t *testing.T) {
	html := string(workspaceHTML)
	js := string(workspaceShellJS)
	for _, want := range []string{
		`data-ws-cache-status-text`, `aria-live="polite"`,
		`function renderCacheBar`, `function updateCacheView`,
		`kernelCache`, `cacheDiagnostics`, `semantix_reuse`,
		`L1 prefix`, `L2 语义切片`, `L3 安全复用`,
	} {
		if !strings.Contains(html, want) && !strings.Contains(js, want) {
			t.Errorf("workspace cache status missing %q", want)
		}
	}
	if strings.Contains(html, "L2 4 slices") || strings.Contains(html, "本轮缓存已复用") {
		t.Error("workspace cache status must not ship fabricated demo metrics")
	}
	if !strings.Contains(html, `data-ws-cache-status aria-live="polite" hidden`) {
		t.Error("workspace cache status must stay hidden until telemetry is observed")
	}
}

func TestServeWorkspaceUsesSemantixWordmark(t *testing.T) {
	logo := string(logoWordmarkSVG)
	if !strings.Contains(logo, `aria-label="Semantix"`) {
		t.Fatal("shared logo must identify Semantix")
	}
	if !strings.Contains(strings.ToLower(logo), "semantix") {
		t.Fatal("shared logo must contain the Semantix wordmark")
	}
	if strings.Contains(logo, "#0153e5") {
		t.Fatal("shared logo still contains the pre-rebrand blue Reasonix asset")
	}
}

func TestServeWorkspaceSideDrawerContract(t *testing.T) {

	for asset, wants := range map[string][]string{
		string(workspaceShellJS):   {"ws-side-open", "data-ws-side-toggle", "Escape", "aria-hidden"},
		string(workspaceLayoutCSS): {"@media (max-width: 900px)", "body.ws-side-open .ws-side", "ws-side-scrim"},
	} {
		for _, want := range wants {
			if !strings.Contains(asset, want) {
				t.Errorf("workspace drawer asset missing %q", want)
			}
		}
	}
}

// TestServeWorkspaceSelectorContract pins the GUI-2 (#405) selector wiring:
// hydration hooks in the page, the fragment-token auth bootstrap (same house
// contract as index), and the exact backend endpoints shell.js may call.
func TestServeWorkspaceSelectorContract(t *testing.T) {
	htmlWants := []string{
		`data-ws-project`, `data-ws-project-name`,
		`data-ws-branch`, `data-ws-branch-menu`,
		`data-ws-model`, `data-ws-model-menu`,
		`data-ws-effort`, `data-ws-effort-menu`,
		`data-ws-notice`,
		// GUI-3 (#406) sidebar
		`data-ws-new-task`, `data-ws-task-list`, `data-ws-side-project-name`,
	}
	for _, want := range htmlWants {
		if !strings.Contains(string(workspaceHTML), want) {
			t.Errorf("workspace page missing selector hook %q", want)
		}
	}

	js := string(workspaceShellJS)
	for _, want := range []string{
		`URLSearchParams(window.location.hash.slice(1))`, // fragment-token bootstrap,
		`/auth/token`, // same house contract as index.html
		`window.history.replaceState`,
		`"/status"`,                  // project name from real backend state
		`"/branches"`,                // branch display from real backend state
		`"/models"`,                  // model list + current + effort
		`"/submit"`,                  // switches reuse the CLI command surface
		`"/model "`,                  //   .../model <ref>
		`"/effort "`,                 //   .../effort <level>
		`"/sessions"`,                // task list = live sessions, no second data model (#406)
		`"/resume"`,                  // task switching keeps session content server-side
		`"/new"`,                     // creating a task enters a fresh session
		`"/workspace/events?live=1"`, // GUI-4 versioned SSE transport
		`"/history"`,                 // durable session hydration before live projection
		`data-ws-session-search`,
		`data-ws-session-project`,
		`data-ws-session-status`,
		`function filterSessions`,
		`function initSessionFilters`,
		`模型不可用`, // explicit unavailable-model signal (#405 acceptance)
	} {
		if !strings.Contains(js, want) {
			t.Errorf("workspace shell.js missing %q", want)
		}
	}

	// Guard rail: the shell must not use the legacy raw stream.
	for _, forbidden := range []string{`"/events"`} {
		if strings.Contains(js, forbidden) {
			t.Errorf("workspace shell.js dials out-of-contract endpoint %s", forbidden)
		}
	}
}

// TestServeWorkspaceWorkflowContract pins the GUI-5 projection boundary. The
// browser must consume the versioned stream, merge by tool identity, and use
// safe text nodes for untrusted event content rather than injecting markup.
func TestServeWorkspaceWorkflowContract(t *testing.T) {
	html := string(workspaceHTML)
	for _, want := range []string{
		`data-ws-timeline`, `data-ws-empty`, `aria-label="Agent 工作流时间线"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("workspace page missing workflow hook %q", want)
		}
	}

	js := string(workspaceShellJS)
	for _, want := range []string{
		`Number.isSafeInteger(payload.seq)`,
		`payload.seq <= lastEventSeq`,
		`toolKey(tool, seq)`,
		`data.kind === "tool_progress"`,
		`renderPlan(parsePlan(tool.args))`,
		`MAX_INLINE_CHARS`,
		`MAX_RENDER_CHARS`,
		`details.addEventListener("toggle"`,
		`function cleanVisibleText`,
		`isProviderAuthError`,
		`if (!workflow.active && isProviderAuthError(statusText))`,
		`if (s.failure) row.title +=`,
		`textContent`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("workflow renderer missing %q", want)
		}
	}
	if strings.Contains(js, `innerHTML`) {
		t.Error("workflow renderer must not inject event content through innerHTML")
	}
	if !strings.Contains(js, `el.empty.hidden = true`) || !strings.Contains(js, `workflow.active`) {
		t.Error("workflow renderer must replace the empty state only when real work begins")
	}
}

// TestServeWorkspaceDiffContract pins GUI-6's source-of-truth boundary: the
// browser renders the server's structured fileDiff rows and copies the exact
// unified diff string, without rebuilding status, counts, or line numbers.
func TestServeWorkspaceDiffContract(t *testing.T) {
	html := string(workspaceHTML)
	for _, want := range []string{`data-ws-context-diff`, `data-ws-diff-list`} {
		if !strings.Contains(html, want) {
			t.Errorf("workspace page missing diff hook %q", want)
		}
	}
	js := string(workspaceShellJS)
	for _, want := range []string{
		`function renderDiff`, `fileDiff.lines`, `fileDiff.diff`,
		`navigator.clipboard.writeText`, `appendDiffRows`,
		`展开剩余`, `ws-diff-line__number`, `ws-syntax-keyword`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("workspace diff renderer missing %q", want)
		}
	}
	if strings.Contains(js, `innerHTML`) {
		t.Error("workspace diff renderer must not inject event content through innerHTML")
	}
	css := string(workspaceLayoutCSS)
	for _, want := range []string{`.ws-diff-view`, `.ws-diff-line--added`, `.ws-diff-view__copy`, `.ws-diff-view__fold`} {
		if !strings.Contains(css, want) {
			t.Errorf("workspace diff styles missing %q", want)
		}
	}
}

// TestServeWorkspaceWorkbenchContract pins GUI-7's context-panel boundary:
// all four views are real tab/panel pairs, and terminal/review data is
// projected from the existing workspace event and approval contracts.
func TestServeWorkspaceWorkbenchContract(t *testing.T) {
	html := string(workspaceHTML)
	for _, want := range []string{
		`data-ws-tab="files"`, `data-ws-tab="diff"`,
		`data-ws-tab="terminal"`, `data-ws-tab="review"`,
		`data-ws-panel="files"`, `data-ws-panel="diff"`,
		`data-ws-panel="terminal"`, `data-ws-panel="review"`,
		`role="tabpanel"`, `aria-controls="ws-panel-review"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("workspace page missing workbench hook %q", want)
		}
	}

	js := string(workspaceShellJS)
	for _, want := range []string{
		`function activateContextTab`, `function initContextTabs`,
		`function syncTreeSelection`, `function renderDiffList`,
		`function renderTerminalList`, `function renderReviewList`,
		`tool.execution`, `execution.exitCode`, `execution.outputTail`,
		`postJSON("/approve"`, `session: false`, `persist: false`,
		`data-ws-review-allow`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("workspace workbench projection missing %q", want)
		}
	}
	if strings.Contains(js, `innerHTML`) {
		t.Error("workspace workbench renderer must not inject event content through innerHTML")
	}

	css := string(workspaceLayoutCSS)
	for _, want := range []string{`.ws-context-panel`, `.ws-terminal-entry`, `.ws-review-entry`, `.ws-context-scroll`} {
		if !strings.Contains(css, want) {
			t.Errorf("workspace workbench styles missing %q", want)
		}
	}
}

// TestServeWorkspaceInteractivePromptContract pins the live decision boundary:
// structured ask requests render as choices in the timeline and are answered
// through the existing /answer route, while approval requests remain separate.
func TestServeWorkspaceInteractivePromptContract(t *testing.T) {
	js := string(workspaceShellJS)
	for _, want := range []string{
		`workflow.asks`, `function renderAskRequest`, `function renderAskCard`,
		`data.kind === "ask_request"`, `postJSON("/answer"`,
		`ws-ask__option`, `aria-pressed`, `record.status === "cancelled"`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("workspace interactive prompt behavior missing %q", want)
		}
	}
	if strings.Contains(js, `var approval = data.approval || data.ask || {};`) {
		t.Error("workspace permission handling must not treat ask payloads as approvals")
	}
	if strings.HasSuffix(strings.TrimSpace(js), "initSessionFilters();") {
		t.Error("workspace session filter initialization must stay inside the shell IIFE")
	}

	css := string(workspaceLayoutCSS)
	for _, want := range []string{`.ws-event--ask`, `.ws-ask__options`, `.ws-ask__option.is-selected`, `.ws-ask__submit`} {
		if !strings.Contains(css, want) {
			t.Errorf("workspace interactive prompt styles missing %q", want)
		}
	}
}

// TestServeWorkspaceComposerContract pins GUI-8's composer boundary: browser
// actions use the existing HTTP routes and never alter permission mode locally.
func TestServeWorkspaceComposerContract(t *testing.T) {
	html := string(workspaceHTML)
	for _, want := range []string{
		`data-ws-composer`, `data-ws-input`, `data-ws-send`, `data-ws-cancel`,
		`data-ws-permission`,
		`type="submit"`, `hidden`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("workspace composer missing hook %q", want)
		}
	}
	js := string(workspaceShellJS)
	for _, want := range []string{
		`function sendComposer`, `function cancelComposer`, `function initComposer`,
		`postJSON("/submit"`, `postJSON("/cancel"`, `data-ws-permission-label`,
		`当前任务正在运行，请等待完成或先中止`, `已取消`, `cancelled`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("workspace composer behavior missing %q", want)
		}
	}
	if strings.Contains(js, `postJSON("/tool-approval-mode"`) || strings.Contains(js, `postJSON("/bypass"`) {
		t.Error("workspace composer must not change permission mode from the browser")
	}
}

// TestServeSessionsExposeInFlight verifies the /sessions payload the sidebar
// consumes: entries keep name/path identity and can flag running sessions via
// the existing branch sidecar marker — still one shared data model (#406).
func TestServeSessionsExposeInFlight(t *testing.T) {
	srv := newWorkspaceTestServer(t)

	resp, err := http.Get(srv.URL + "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /sessions status = %d", resp.StatusCode)
	}

	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decode /sessions: %v", err)
	}
	for i, row := range rows {
		if _, ok := row["name"].(string); !ok {
			t.Errorf("/sessions[%d] missing string name", i)
		}
		if _, ok := row["path"].(string); !ok {
			t.Errorf("/sessions[%d] missing string path", i)
		}
		for key, want := range map[string]string{"in_flight": "bool", "current": "bool", "turns": "number", "title": "string", "status": "string", "project": "string", "updated_at": "string", "failure": "string"} {
			v, ok := row[key]
			if !ok {
				continue
			}
			switch want {
			case "bool":
				if _, ok := v.(bool); !ok {
					t.Errorf("/sessions[%d].%s = %#v, want bool", i, key, v)
				}
			case "number":
				if _, ok := v.(float64); !ok {
					t.Errorf("/sessions[%d].%s = %#v, want number", i, key, v)
				}
			case "string":
				if _, ok := v.(string); !ok {
					t.Errorf("/sessions[%d].%s = %#v, want string", i, key, v)
				}
			}
		}
	}
}

// TestServeModelsExposesEffort ensures GET /models carries the active
// provider's reasoning effort so the effort chip reflects real config state.
func TestServeModelsExposesEffort(t *testing.T) {
	srv := newWorkspaceTestServer(t)

	resp, err := http.Get(srv.URL + "/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /models status = %d", resp.StatusCode)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"current", "label", "default", "effort", "models"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("GET /models response missing %q", key)
		}
	}
	if _, ok := payload["effort"].(string); !ok {
		t.Errorf("GET /models effort = %#v, want string", payload["effort"])
	}
}

func TestServeWorkspaceAssets(t *testing.T) {
	srv := newWorkspaceTestServer(t)

	for path, wantCT := range map[string]string{
		"/workspace/tokens.css": "text/css",
		"/workspace/layout.css": "text/css",
		"/workspace/shell.js":   "text/javascript",
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s status = %d", path, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, wantCT) {
			t.Errorf("GET %s content-type = %q, want prefix %q", path, ct, wantCT)
		}
	}
}

// TestServeWorkspaceDesignTokens keeps the brand tokens aligned with their
// sources of truth (site palette, mockup semantics).
func TestServeWorkspaceDesignTokens(t *testing.T) {
	var needs = map[string][]string{
		string(workspaceTokensCSS): {"--sx-green: oklch(0.608 0.14 165)", "--sx-orange:", "--sx-bg-canvas:"},
		string(workspaceLayoutCSS): {`@media (max-width: 1440px)`, `@media (max-width: 900px)`},
	}
	for asset, wants := range needs {
		for _, want := range wants {
			if !strings.Contains(asset, want) {
				t.Errorf("workspace asset missing %q", want)
			}
		}
	}
	css := string(workspaceTokensCSS)
	for _, want := range []string{"--sx-side-w: 296px", "--sx-context-w: 548px"} {
		if !strings.Contains(css, want) {
			t.Errorf("workspace geometry token missing %q", want)
		}
	}
	layout := string(workspaceLayoutCSS)
	for _, want := range []string{"grid-template-rows: minmax(0, 1fr) auto", "grid-column: 1", "data-ws-state=\"empty\""} {
		if !strings.Contains(layout, want) {
			t.Errorf("workspace layout contract missing %q", want)
		}
	}
}

// TestServeWorkspacePanelRecoveryContract pins the epic #403 core-usability
// fixes in the workspace shell:
//   - the /sessions "recovered" status renders a pill instead of crashing the
//     task list (TASK_PILLS key + defensive fallback);
//   - the composer running latch is re-synced from /status when a turn ends or
//     aborts instead of staying disabled forever;
//   - a JSON replay window (/workspace/replay) rebuilds the Diff/Terminal/
//     Review panels after a refresh without duplicating timeline cards.
func TestServeWorkspacePanelRecoveryContract(t *testing.T) {
	js := string(workspaceShellJS)
	for _, want := range []string{
		`recovered: ["待恢复", "ws-state-recovered"]`,
		`TASK_PILLS[stateKey] || TASK_PILLS.empty`,
		`function refreshRunningState`,
		`refreshRunningState(); // #403: turn finished`,
		`refreshRunningState(); // #403: turn aborted`,
		`"/workspace/replay"`,
		`function loadWorkspaceReplay`,
		`function applyReplayFrames`,
		`must NOT advance the shared lastEventSeq`,
		`if (record.card) renderTool(record.card, record);`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("workspace shell.js missing %q", want)
		}
	}
	// Replay must stay a panel-only projection: timeline user/assistant/tool
	// cards are owned by /history + live frames, so applyReplayFrames may never
	// create timeline cards (it only feeds workflow state into the context
	// panels). The function is bounded by the next function declaration.
	replayBody := strings.Split(strings.Split(js, "function applyReplayFrames")[1], "function connectWorkspaceEvents")[0]
	for _, forbidden := range []string{`makeEvent(`, `el.timeline`, `addOptimisticUserMessage`} {
		if strings.Contains(replayBody, forbidden) {
			t.Errorf("applyReplayFrames must not render timeline cards, found %q", forbidden)
		}
	}

	css := string(workspaceLayoutCSS)
	for _, want := range []string{".ws-state-recovered", "var(--sx-orange-dim)", "var(--sx-orange-border)"} {
		if !strings.Contains(css, want) {
			t.Errorf("workspace layout.css missing %q", want)
		}
	}
}
