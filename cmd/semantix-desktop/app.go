package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/mod/semver"

	"semantix/harness/agent"
	"semantix/harness/boot"
	"semantix/harness/config"
	"semantix/harness/control"
	"semantix/harness/serve"
	"semantix/harness/webapp"
)

var desktopVersion = "0.1.0"

type recentProject struct {
	Path       string `json:"path"`
	LastOpened string `json:"lastOpened"`
}

type desktopState struct {
	Recent []recentProject `json:"recent"`
}

type desktopRuntime struct {
	controller *control.Controller
	leases     *control.SessionLeaseKeeper
	cancel     context.CancelFunc
	done       chan error
}

type App struct {
	ctx      context.Context
	mu       sync.Mutex
	starting bool
	run      *desktopRuntime
}

var buildDesktopController = func(ctx context.Context, opts boot.Options) (*control.Controller, error) {
	return boot.Build(ctx, opts)
}

func newDesktopApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go a.checkForUpdates(ctx)
}

func (a *App) checkForUpdates(ctx context.Context) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/Gnosil/semantix/releases/latest", nil)
	if err != nil {
		return
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "Semantix-Desktop/"+desktopVersion)
	client := &http.Client{Timeout: 8 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return
	}
	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if json.NewDecoder(response.Body).Decode(&release) != nil || !strings.HasPrefix(release.HTMLURL, "https://github.com/Gnosil/semantix/releases/") {
		return
	}
	latest := release.TagName
	if !strings.HasPrefix(latest, "v") {
		latest = "v" + latest
	}
	if !semver.IsValid(latest) || semver.Compare(latest, "v"+desktopVersion) <= 0 {
		return
	}
	choice, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
		Type: runtime.InfoDialog, Title: "Semantix 有新版本",
		Message: "发现 " + release.TagName + "。更新不会自动安装。",
		Buttons: []string{"稍后", "打开下载页"}, DefaultButton: "打开下载页", CancelButton: "稍后",
	})
	if err == nil && choice == "打开下载页" {
		runtime.BrowserOpenURL(ctx, release.HTMLURL)
	}
}

func (a *App) RecentProjects() []recentProject {
	state, _ := loadDesktopState()
	return state.Recent
}

func (a *App) OpenProject() (bool, error) {
	path, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "选择 Semantix 项目文件夹"})
	if err != nil {
		return false, err
	}
	if path == "" {
		return false, nil
	}
	return true, a.OpenRecent(path)
}

func (a *App) OpenRecent(path string) error {
	root, err := canonicalProjectPath(path)
	if err != nil {
		return err
	}
	a.mu.Lock()
	if a.starting || a.run != nil {
		a.mu.Unlock()
		return errors.New("当前窗口已经绑定项目")
	}
	a.starting = true
	a.mu.Unlock()

	run, address, token, err := startDesktopRuntime(a.ctx, root)
	a.mu.Lock()
	a.starting = false
	if err != nil {
		a.mu.Unlock()
		return err
	}
	a.run = run
	a.mu.Unlock()
	if err := rememberProject(root); err != nil {
		// The project is already usable; a recent-list failure is non-fatal.
		fmt.Fprintln(os.Stderr, "desktop: save recent project:", err)
	}
	runtime.WindowSetTitle(a.ctx, "Semantix — "+filepath.Base(root))
	target := "http://" + address + "/workspace#token=" + url.QueryEscape(token)
	encoded, _ := json.Marshal(target)
	runtime.WindowExecJS(a.ctx, "window.location.replace("+string(encoded)+")")
	return nil
}

func (a *App) beforeClose(ctx context.Context) bool {
	a.mu.Lock()
	run := a.run
	a.mu.Unlock()
	if run == nil || !run.controller.RuntimeStatus().Running {
		return false
	}
	choice, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
		Type: runtime.QuestionDialog, Title: "任务仍在运行",
		Message: "关闭窗口会取消当前任务。",
		Buttons: []string{"返回任务", "取消任务并退出"}, DefaultButton: "返回任务", CancelButton: "返回任务",
	})
	if err != nil || choice != "取消任务并退出" {
		return true
	}
	run.controller.Cancel()
	return false
}

func (a *App) shutdown(context.Context) {
	a.mu.Lock()
	run := a.run
	a.run = nil
	a.mu.Unlock()
	if run == nil {
		return
	}
	run.cancel()
	select {
	case <-run.done:
	case <-time.After(12 * time.Second):
	}
	run.controller.Close()
	run.leases.Release()
}

func startDesktopRuntime(parent context.Context, root string) (*desktopRuntime, string, string, error) {
	bc := serve.NewBroadcaster()
	resumePath, resumeModel := mostRecentProjectSession(root)
	ctrl, err := buildDesktopController(parent, boot.Options{Model: resumeModel, Sink: bc, WorkspaceRoot: root, StatsSource: "desktop"})
	if err != nil {
		return nil, "", "", fmt.Errorf("启动 Agent 失败: %w", err)
	}
	cleanup := func() { ctrl.Close() }
	leases := control.NewSessionLeaseKeeper()
	var resumeSession *agent.Session
	if resumePath != "" {
		resumeSession, err = agent.LoadSession(resumePath)
		if err != nil {
			resumePath = ""
		}
	}
	if resumePath != "" {
		if err := leases.Rebind(resumePath); err != nil {
			cleanup()
			return nil, "", "", err
		}
		ctrl.Resume(resumeSession, resumePath)
	} else {
		ctrl.EnsureSessionPath()
		if err := leases.Rebind(ctrl.SessionPath()); err != nil {
			cleanup()
			return nil, "", "", err
		}
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		leases.Release()
		cleanup()
		return nil, "", "", err
	}
	token, err := randomToken()
	if err != nil {
		_ = listener.Close()
		leases.Release()
		cleanup()
		return nil, "", "", err
	}
	serveCfg := config.ServeConfig{AuthMode: "token", Token: token}
	srv, err := webapp.Assemble(ctrl, bc, serveCfg, leases, listener.Addr().String())
	if err != nil {
		_ = listener.Close()
		leases.Release()
		cleanup()
		return nil, "", "", err
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan error, 1)
	go func() { done <- srv.RunGracefulListener(ctx, listener) }()
	return &desktopRuntime{controller: ctrl, leases: leases, cancel: cancel, done: done}, listener.Addr().String(), token, nil
}

func mostRecentProjectSession(root string) (string, string) {
	sessions, err := agent.ListSessions(config.SessionDir())
	if err != nil {
		return "", ""
	}
	for _, session := range sessions {
		if session.WorkspaceRoot == "" || !strings.EqualFold(filepath.Clean(session.WorkspaceRoot), root) {
			continue
		}
		model, _ := agent.LoadSessionModel(session.Path)
		return session.Path, model
	}
	return "", ""
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func canonicalProjectPath(path string) (string, error) {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("所选路径不是文件夹")
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = filepath.Clean(resolved)
	}
	return abs, nil
}

func desktopStatePath() string { return filepath.Join(config.SemantixHomeDir(), "desktop.json") }

func loadDesktopState() (desktopState, error) {
	var state desktopState
	data, err := os.ReadFile(desktopStatePath())
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return desktopState{}, err
	}
	if len(state.Recent) > 10 {
		state.Recent = state.Recent[:10]
	}
	return state, nil
}

func rememberProject(path string) error {
	state, _ := loadDesktopState()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	next := []recentProject{{Path: path, LastOpened: now}}
	for _, project := range state.Recent {
		if !strings.EqualFold(project.Path, path) {
			next = append(next, project)
		}
	}
	sort.SliceStable(next, func(i, j int) bool { return next[i].LastOpened > next[j].LastOpened })
	if len(next) > 10 {
		next = next[:10]
	}
	data, err := json.MarshalIndent(desktopState{Recent: next}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(config.SemantixHomeDir(), 0o700); err != nil {
		return err
	}
	tmp := desktopStatePath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, desktopStatePath())
}
