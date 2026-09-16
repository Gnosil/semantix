package slice

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func seedPruneStore(t *testing.T, path string, slices ...*Slice) {
	t.Helper()
	st, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestStore(st)
	for _, s := range slices {
		if err := st.Put(s); err != nil {
			t.Fatal(err)
		}
	}
}

type pruneFileSnapshot struct {
	Data  string
	Mtime int64
	Mode  os.FileMode
}

func snapshotPruneDir(t *testing.T, dir string) map[string]pruneFileSnapshot {
	t.Helper()
	out := map[string]pruneFileSnapshot{}
	err := filepath.WalkDir(dir, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fi, err := e.Info()
		if err != nil {
			return err
		}
		out[path] = pruneFileSnapshot{string(b), fi.ModTime().UnixNano(), fi.Mode()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPrunePreviewIsStrictlyReadOnly(t *testing.T) {
	for _, fixture := range []string{"missing", "healthy", "base-only", "corrupt-base", "corrupt-journal", "mismatch", "unknown-field", "torn-tail"} {
		t.Run(fixture, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "s.jsonl")
			if fixture != "missing" {
				seedPruneStore(t, path, pruneSlice("old", 180))
				switch fixture {
				case "base-only":
					st := reopen(t, path)
					if err := st.(*fileStore).Compact(); err != nil {
						t.Fatal(err)
					}
					closeTestStore(st)
					if err := os.Remove(path + ".journal"); err != nil {
						t.Fatal(err)
					}
					if err := os.Remove(path + ".maintenance.lock"); err != nil {
						t.Fatal(err)
					}
				case "corrupt-base":
					if err := os.WriteFile(path, []byte("bad json"), 0600); err != nil {
						t.Fatal(err)
					}
				case "corrupt-journal":
					if err := os.WriteFile(path+".journal", []byte("bad header"), 0600); err != nil {
						t.Fatal(err)
					}
				case "mismatch":
					if err := os.WriteFile(path, []byte("\n"), 0600); err != nil {
						t.Fatal(err)
					}
				case "unknown-field", "torn-tail":
					f, err := os.OpenFile(path+".journal", os.O_APPEND|os.O_WRONLY, 0600)
					if err != nil {
						t.Fatal(err)
					}
					line := "{\"op\":\"put\",\"s\":{\"ID\":\"future\",\"future_field\":true}}\n"
					if fixture == "torn-tail" {
						line = "{\"op\":\"del\""
					}
					_, err = f.WriteString(line)
					f.Close()
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			before := snapshotPruneDir(t, dir)
			r, err := PruneFile(path, PruneOptions{Scope: Project, Now: pruneNow})
			wantError := fixture != "missing" && fixture != "healthy" && fixture != "base-only"
			if (err != nil) != wantError {
				t.Fatalf("error %v; plan %+v", err, r)
			}
			if !reflect.DeepEqual(before, snapshotPruneDir(t, dir)) {
				t.Fatal("preview changed files or mtimes")
			}
		})
	}
}

func TestPruneApplyArchivesAndDeletesAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	a, b := pruneSlice("a", 60), pruneSlice("b", 60)
	b.Content = a.Content
	b.Embedding = []float32{0.125, 0.75}
	b.Stats.Misses = 3
	old := pruneSlice("old", 180)
	user := pruneSlice("user", 180)
	user.Scope = User
	seedPruneStore(t, path, a, b, old, user)
	// Verify the batch works with records already folded into a base too.
	st := reopen(t, path)
	if err := st.(*fileStore).Compact(); err != nil {
		t.Fatal(err)
	}
	closeTestStore(st)
	opts := PruneOptions{Scope: Project, Now: pruneNow}
	preview, err := PruneFile(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	opts.Apply = true
	r, err := PruneFile(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	if r.Removed != 2 || r.ArchivePath == "" || !reflect.DeepEqual(preview.Candidates, r.Candidates) {
		t.Fatalf("apply: %+v", r)
	}
	archive, err := os.ReadFile(r.ArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	var archived Slice
	if err := json.Unmarshal(bytes.Split(archive, []byte{'\n'})[0], &archived); err != nil {
		t.Fatal(err)
	}
	if archived.ID != "b" || !reflect.DeepEqual(archived.Embedding, b.Embedding) || archived.Stats != b.Stats || !reflect.DeepEqual(archived.Meta, b.Meta) {
		t.Fatalf("archive changed record: %+v", archived)
	}
	st = reopen(t, path)
	all, err := st.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("survivors: %+v", all)
	}
	got, _ := st.Get("a")
	if !reflect.DeepEqual(got, a) {
		t.Fatal("representative modified")
	}
	if err := st.Put(pruneSlice("after", 1)); err != nil {
		t.Fatal(err)
	}
	closeTestStore(st)
	before := snapshotPruneDir(t, filepath.Dir(path))
	again, err := PruneFile(path, opts)
	if err != nil || again.Removed != 0 {
		t.Fatalf("repeat: %+v %v", again, err)
	}
	if !reflect.DeepEqual(before, snapshotPruneDir(t, filepath.Dir(path))) {
		t.Fatal("empty apply wrote another archive/journal")
	}
	// Recovery uses existing admission/trust semantics, not a trust bypass.
	recovery := reopen(t, filepath.Join(t.TempDir(), "restore.jsonl"))
	n, skipped, err := Import(recovery, bytes.NewReader(archive), OriginImport)
	if err != nil || n != 2 || skipped != 0 {
		t.Fatalf("recovery %d %d %v", n, skipped, err)
	}
}

func TestPrunePrecommitFailuresLeaveActiveStoreUntouched(t *testing.T) {
	for _, stage := range []string{"archive-create", "archive-write", "archive-sync", "archive-close", "journal-create", "journal-write", "journal-short-write", "journal-sync", "journal-close", "rename"} {
		t.Run(stage, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "s.jsonl")
			seedPruneStore(t, path, pruneSlice("old1", 180), pruneSlice("old2", 180))
			base, _ := os.ReadFile(path)
			journal, _ := os.ReadFile(path + ".journal")
			ops := defaultPruneIO()
			fault := errors.New("injected failure")
			which := func(name string) string {
				if strings.Contains(name, "prune-archive-") {
					return "archive"
				}
				return "journal"
			}
			ops.createTemp = func(dir, pattern string) (*os.File, error) {
				if stage == which(pattern)+"-create" {
					return nil, fault
				}
				return os.CreateTemp(dir, pattern)
			}
			ops.write = func(f *os.File, b []byte) (int, error) {
				if stage == which(f.Name())+"-write" {
					_, _ = f.Write(b[:len(b)/2])
					return len(b) / 2, fault
				}
				if stage == which(f.Name())+"-short-write" {
					return f.Write(b[:len(b)/2])
				}
				return f.Write(b)
			}
			ops.sync = func(f *os.File) error {
				if stage == which(f.Name())+"-sync" {
					return fault
				}
				return f.Sync()
			}
			ops.close = func(f *os.File) error {
				if stage == which(f.Name())+"-close" {
					f.Close()
					return fault
				}
				return f.Close()
			}
			ops.rename = func(a, b string) error {
				if stage == "rename" {
					return fault
				}
				return os.Rename(a, b)
			}
			r, err := pruneFile(path, PruneOptions{Scope: Project, Apply: true, Now: pruneNow}, ops)
			if err == nil || r.Removed != 0 {
				t.Fatalf("fault committed: %+v %v", r, err)
			}
			afterBase, _ := os.ReadFile(path)
			afterJournal, _ := os.ReadFile(path + ".journal")
			if !bytes.Equal(base, afterBase) || !bytes.Equal(journal, afterJournal) {
				t.Fatal("active bytes changed after failure")
			}
			st := reopen(t, path)
			all, _ := st.ListAll()
			if len(all) != 2 {
				t.Fatal("partial deletion after reopen")
			}
			if err := st.UpdateStats("old1", SliceStats{Hits: 1, LastUsed: pruneNow}); err != nil {
				t.Fatal(err)
			}
			closeTestStore(st)
			left, err := filepath.Glob(path + ".prune-journal-*")
			if err != nil || len(left) != 0 {
				t.Fatal("temporary journal leaked")
			}
		})
	}
}

func TestPruneExcludesLiveAndClosedWriterHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	seedPruneStore(t, path, pruneSlice("old", 180))
	st := reopen(t, path)
	before := snapshotPruneDir(t, filepath.Dir(path))
	for _, apply := range []bool{false, true} {
		if _, err := PruneFile(path, PruneOptions{Scope: Project, Apply: apply, Now: pruneNow}); !errors.Is(err, ErrStoreBusy) {
			t.Fatalf("live writer allowed: %v", err)
		}
	}
	if !reflect.DeepEqual(before, snapshotPruneDir(t, filepath.Dir(path))) {
		t.Fatal("busy maintenance changed files")
	}
	if err := st.UpdateStats("old", SliceStats{Hits: 1, LastUsed: pruneNow}); err != nil {
		t.Fatal(err)
	}
	closeTestStore(st)
	r, err := PruneFile(path, PruneOptions{Scope: Project, Apply: true, Now: pruneNow})
	if err != nil || r.Removed != 0 {
		t.Fatalf("lost usage: %+v %v", r, err)
	}
	if err := st.Put(pruneSlice("zombie", 180)); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed stale writer revived: %v", err)
	}
	lease, err := lockStore(path, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStore(path); !errors.Is(err, ErrStoreBusy) {
		lease.Close()
		t.Fatalf("writer entered exclusive maintenance: %v", err)
	}
	lease.Close()
}

func TestPruneLeaseHelper(t *testing.T) {
	path := os.Getenv("SEMANTIX_PRUNE_LEASE_TEST_DB")
	if path == "" {
		return
	}
	st, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestStore(st)
	fmt.Println("lease-ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestPruneCrossProcessLeaseAndCrashRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	seedPruneStore(t, path, pruneSlice("old", 180))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPruneLeaseHelper$")
	cmd.Env = append(os.Environ(), "SEMANTIX_PRUNE_LEASE_TEST_DB="+path)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "lease-ready" {
		t.Fatalf("helper: %q %v", line, err)
	}
	if _, err := PruneFile(path, PruneOptions{Scope: Project, Apply: true, Now: pruneNow}); !errors.Is(err, ErrStoreBusy) {
		t.Fatalf("cross-process exclusion failed: %v", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	r, err := PruneFile(path, PruneOptions{Scope: Project, Apply: true, Now: pruneNow})
	if err != nil || r.Removed != 1 {
		t.Fatalf("crashed process retained lease: %+v %v", r, err)
	}
}

func TestPruneCommitCrashHelper(t *testing.T) {
	path := os.Getenv("SEMANTIX_PRUNE_CRASH_TEST_DB")
	if path == "" {
		return
	}
	ops := defaultPruneIO()
	ops.rename = func(from, to string) error {
		if os.Getenv("SEMANTIX_PRUNE_CRASH_TEST_STAGE") == "after" {
			if err := os.Rename(from, to); err != nil {
				return err
			}
		}
		os.Exit(23) // simulate process death immediately beside the commit point
		return nil
	}
	_, err := pruneFile(path, PruneOptions{Scope: Project, Apply: true, Now: pruneNow}, ops)
	t.Fatalf("expected process death, got %v", err)
}

func TestPruneCrashAtCommitIsAllOrNothing(t *testing.T) {
	for _, stage := range []string{"before", "after"} {
		t.Run(stage, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "s.jsonl")
			seedPruneStore(t, path, pruneSlice("old-a", 180), pruneSlice("old-b", 180), pruneSlice("recent", 1))
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPruneCommitCrashHelper$")
			cmd.Env = append(os.Environ(), "SEMANTIX_PRUNE_CRASH_TEST_DB="+path, "SEMANTIX_PRUNE_CRASH_TEST_STAGE="+stage)
			out, err := cmd.CombinedOutput()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 23 {
				t.Fatalf("helper: %v %s", err, out)
			}
			st := reopen(t, path)
			all, err := st.ListAll()
			if err != nil {
				t.Fatal(err)
			}
			want := 3
			if stage == "after" {
				want = 1
			}
			if len(all) != want {
				t.Fatalf("partial transaction: got %d want %d", len(all), want)
			}
			if got, _ := st.Get("recent"); got == nil {
				t.Fatal("survivor lost")
			}
			archives, err := filepath.Glob(path + ".prune-archive-*.jsonl")
			if err != nil || len(archives) != 1 {
				t.Fatal("durable recovery archive missing")
			}
			b, err := os.ReadFile(archives[0])
			if err != nil || bytes.Count(b, []byte{'\n'}) != 2 {
				t.Fatal("archive incomplete at commit")
			}
		})
	}
}

type pruneReadFailure struct{}

func (pruneReadFailure) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestPruneJSONLDoesNotHideReadFailure(t *testing.T) {
	r := bufio.NewReader(io.MultiReader(strings.NewReader(`{"ID":"otherwise-valid"}`), pruneReadFailure{}))
	if _, _, err := readJSONLLine(r); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("partial read accepted: %v", err)
	}
}
