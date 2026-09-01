package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	workspaceTreeMaxDepth = 3
	workspaceTreeMaxItems = 500
)

type workspaceTreeEntry struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Directory bool   `json:"directory"`
	Depth     int    `json:"depth"`
}

// workspaceTree exposes a bounded, read-only view of the selected workspace.
// It deliberately walks only real directories and never follows symlinks, so
// the browser can render the Reasonix-style file panel without gaining a file
// read primitive or leaving the configured project root.
func (s *Server) workspaceTree(w http.ResponseWriter, _ *http.Request) {
	root := filepath.Clean(strings.TrimSpace(s.ctl().WorkspaceRoot()))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if root == "." || root == "" {
		_, _ = w.Write([]byte(`{"root":"","entries":[]}`))
		return
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		_, _ = w.Write([]byte(`{"root":"","entries":[]}`))
		return
	}

	entries := make([]workspaceTreeEntry, 0, 64)
	var walk func(string, int)
	walk = func(dir string, depth int) {
		if len(entries) >= workspaceTreeMaxItems || depth > workspaceTreeMaxDepth {
			return
		}
		children, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		sort.Slice(children, func(i, j int) bool {
			if children[i].IsDir() != children[j].IsDir() {
				return children[i].IsDir()
			}
			return strings.ToLower(children[i].Name()) < strings.ToLower(children[j].Name())
		})
		for _, child := range children {
			if len(entries) >= workspaceTreeMaxItems || shouldSkipWorkspaceTreeName(child.Name()) {
				continue
			}
			childPath := filepath.Join(dir, child.Name())
			rel, err := filepath.Rel(root, childPath)
			if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			entry := workspaceTreeEntry{
				Path:      filepath.ToSlash(rel),
				Name:      child.Name(),
				Directory: child.IsDir(),
				Depth:     depth,
			}
			entries = append(entries, entry)
			if child.IsDir() && depth < workspaceTreeMaxDepth {
				walk(childPath, depth+1)
			}
		}
	}
	walk(root, 0)

	response := struct {
		Root    string               `json:"root"`
		Entries []workspaceTreeEntry `json:"entries"`
	}{Root: root, Entries: entries}
	_ = json.NewEncoder(w).Encode(response)
}

func shouldSkipWorkspaceTreeName(name string) bool {
	switch strings.ToLower(name) {
	case ".git", ".hg", ".svn", ".tmp", "node_modules", "vendor", "dist", "build", "target":
		return true
	default:
		return false
	}
}
