package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"semantix/kernel/config"
	"semantix/kernel/slice"
)

func TestSharedStoreAcrossCommands(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		t.Run(map[bool]string{false: "scope_default", true: "home_config"}[pinned], func(t *testing.T) {
			t.Chdir(t.TempDir())
			home := t.TempDir()
			key := "HOME"
			if runtime.GOOS == "windows" {
				key = "USERPROFILE"
			}
			t.Setenv(key, home)
			clearDoctorEnv(t)
			t.Setenv("SEMANTIX_SCOPE", "user")
			wantDB := filepath.Join(home, ".semantix", "user.db")
			if pinned {
				dir := filepath.Join(home, ".semantix")
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "semantix.toml"), []byte("[store]\ndb = '~/shared.db'\nscope = 'user'\n"), 0600); err != nil {
					t.Fatal(err)
				}
				wantDB = filepath.Join(home, "shared.db")
			}
			line, _ := json.Marshal(map[string]string{"role": "user", "content": "正常中文视频渲染"})
			if err := os.WriteFile("input.jsonl", line, 0600); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{
				{"extract", "--input", "input.jsonl"},
				{"lookup", "--query", "视频渲染"},
				{"inject", "--query", "视频渲染"},
				{"search", "--query", "视频渲染", "--retriever", "bm25"},
				{"export"},
			} {
				var out, stderr bytes.Buffer
				if code := run(args, &out, &stderr, productionDependencies()); code != 0 {
					t.Fatalf("%v exit=%d stderr=%s", args, code, stderr.String())
				}
				if args[0] == "export" {
					var sl slice.Slice
					if err := json.Unmarshal(out.Bytes(), &sl); err != nil || string(sl.Content) != "正常中文视频渲染" {
						t.Fatalf("export did not read shared content: %s err=%v", out.String(), err)
					}
				} else if args[0] != "extract" && !strings.Contains(out.String(), "视频渲染") {
					t.Fatalf("%v did not read shared content: %s", args, out.String())
				}
			}
			if _, err := os.Stat(wantDB); err != nil {
				t.Fatalf("expected store %s: %v", wantDB, err)
			}
			var out, stderr bytes.Buffer
			if code := runDoctor([]string{"--json"}, &out, &stderr); code != 0 {
				t.Fatalf("doctor exit=%d output=%s", code, out.String())
			}
			var report struct{ Data doctorReport }
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if report.Data.DB != wantDB {
				t.Fatalf("doctor db=%q want %q", report.Data.DB, wantDB)
			}
			for _, args := range [][]string{
				{"gc", "--dry-run", "--json"},
				{"prune", "--dry-run", "--json"},
				{"dashboard", "--json"},
			} {
				out.Reset()
				if code := run(args, &out, &stderr, productionDependencies()); code != 0 {
					t.Fatalf("%v failed: %s", args, stderr.String())
				}
				var envelope struct {
					Data struct {
						Checked int
						Library struct{ Slices int }
					}
				}
				if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				if args[0] == "dashboard" {
					if envelope.Data.Library.Slices != 1 {
						t.Fatalf("dashboard read another store: %s", out.String())
					}
				} else if envelope.Data.Checked != 1 {
					t.Fatalf("%v read another store: %s", args, out.String())
				}
			}
		})
	}
}

func TestExplicitStoreFlagsOverrideConfig(t *testing.T) {
	cfg := &config.Resolved{Fields: []config.Field{{Key: "store.db", Value: "configured.db", Source: config.SourceFile}}}
	for _, tc := range []struct {
		scope slice.Scope
		args  []string
		want  string
	}{
		{slice.User, nil, "configured.db"},
		{slice.Project, nil, "configured.db"},
		{slice.User, []string{"--user-db", "user-flag.db"}, "user-flag.db"},
		{slice.Project, []string{"--project-db", "project-flag.db"}, "project-flag.db"},
		{slice.User, []string{"--user-db", "user-flag.db", "--db", "override.db"}, "override.db"},
	} {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		db := fs.String("db", "", "")
		project := fs.String("project-db", "project.db", "")
		user := fs.String("user-db", "user.db", "")
		if err := fs.Parse(tc.args); err != nil {
			t.Fatal(err)
		}
		if got := selectConfiguredDB(fs, cfg, tc.scope, *db, *project, *user); got != tc.want {
			t.Errorf("%v scope=%v got=%q want=%q", tc.args, tc.scope, got, tc.want)
		}
	}
}
