package semantix

import (
	"reflect"
	"strings"
	"testing"

	"semantix/kernel/bm25"
)

func TestCleanRetrievalQueryExtractsIssueAndDropsExecutionPolicy(t *testing.T) {
	raw := `You are working in a git checkout of the org/repo repository at commit abc. Resolve the GitHub issue below.
<issue>
Cache invalidation fails in RedisStore
</issue>
Requirements:
- implement a complete fix

<execution-policy preset="balanced">
route=direct risk=low verify=targeted review=conditional
</execution-policy>`
	got := bm25.Tokenize(cleanRetrievalQuery(raw))
	want := []string{"cache", "invalidation", "fails", "redisstore"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cleaned tokens = %v, want %v", got, want)
	}
}

func TestBuildRetrievalQueryExtractsStructuredSWEIssueSignals(t *testing.T) {
	raw := `You are working in a git checkout of the django/django repository at commit abc. Resolve the GitHub issue below.
<issue>
inspect.signature() returns incorrect signature on manager methods.
	The wrapper in django/db/models/manager.py exposes (*args, **kwargs) for
	Person.objects.bulk_create. Use functools.wraps instead of assigning __name__
	and __doc__. The example imports inspect and uses from django.db import models.
	See https://github.com/django/django/blob/main/django/db/models/manager.py#L84.
	If acknowledged, assign the ticket to me.
</issue>
Requirements:
- implement a complete fix
<execution-policy preset="balanced">route=full-plan</execution-policy>`

	got := buildRetrievalQuery(raw)
	if got.Strategy != "structured" || got.FallbackReason != "" {
		t.Fatalf("strategy = %q fallback = %q", got.Strategy, got.FallbackReason)
	}
	if got.Intent != "inspect.signature() returns incorrect signature on manager methods." {
		t.Fatalf("intent = %q", got.Intent)
	}
	if got.Repo != "django/django" {
		t.Fatalf("repo = %q", got.Repo)
	}
	if !reflect.DeepEqual(got.Paths, []string{"django/db/models/manager.py"}) {
		t.Fatalf("paths = %v", got.Paths)
	}
	for _, symbol := range []string{"__doc__", "__name__", "functools.wraps", "inspect.signature", "person.objects.bulk_create"} {
		if !containsString(got.Symbols, symbol) {
			t.Errorf("symbols = %v, missing %q", got.Symbols, symbol)
		}
	}
	for _, dependency := range []string{"django", "inspect"} {
		if !containsString(got.Dependencies, dependency) {
			t.Errorf("dependencies = %v, missing %q", got.Dependencies, dependency)
		}
	}
	// Only runner framing is excluded. Prose inside the issue is preserved;
	// dropping it on the assumption that it is noise can drop requirements too.
	for _, noise := range []string{"requirements", "full", "plan"} {
		if containsString(bm25.Tokenize(got.Text), noise) {
			t.Errorf("structured query leaked noise %q: %q", noise, got.Text)
		}
	}
}

func TestBuildRetrievalQueryExtractsErrorAndTestNames(t *testing.T) {
	got := buildRetrievalQuery(`<issue>
Fix HTTP_500 from TestCacheInvalidation in tests/cache/test_backend.py.
Running test_redis_timeout raises ConnectionError in RedisStore.get_value.
</issue>`)
	if got.Strategy != "structured" {
		t.Fatalf("strategy = %q", got.Strategy)
	}
	for _, code := range []string{"connectionerror", "http_500"} {
		if !containsString(got.ErrorCodes, code) {
			t.Errorf("error codes = %v, missing %q", got.ErrorCodes, code)
		}
	}
	for _, name := range []string{"testcacheinvalidation", "test_redis_timeout", "tests/cache/test_backend.py"} {
		if !containsString(got.TestNames, name) {
			t.Errorf("test names = %v, missing %q", got.TestNames, name)
		}
	}
}

func TestBuildRetrievalQueryFallsBackToCleanLexicalQuery(t *testing.T) {
	got := buildRetrievalQuery("Please fix cache invalidation bug")
	if got.Strategy != "lexical_fallback" || got.FallbackReason != "no_structured_signals" {
		t.Fatalf("strategy = %q fallback = %q", got.Strategy, got.FallbackReason)
	}
	if got.Text != cleanRetrievalQuery("Please fix cache invalidation bug") {
		t.Fatalf("text = %q", got.Text)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if strings.EqualFold(item, want) {
			return true
		}
	}
	return false
}

func TestCleanRetrievalQueryKeepsDomainAndCJKTerms(t *testing.T) {
	got := bm25.Tokenize(cleanRetrievalQuery("Please fix CacheKey 测试失败 issue"))
	want := []string{"fix", "cachekey", "测", "试", "失", "败"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cleaned tokens = %v, want %v", got, want)
	}
}

func TestCleanRetrievalQueryFailsClosedWhenOnlyFramingRemains(t *testing.T) {
	if got := cleanRetrievalQuery("Please resolve the GitHub issue below"); got != "" {
		t.Fatalf("cleaned query = %q, want empty", got)
	}
}

func TestBuildRetrievalQueryKeepsPlainMultilineTask(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want []string
	}{
		{
			name: "environment_before_task",
			raw:  "Workspace contains src/cache/store.go.\nRefresh cached permissions when membership changes.\nKeep revoked access unavailable.",
			want: []string{"refresh", "permissions", "membership", "revoked", "unavailable"},
		},
		{
			name: "task_continues_after_first_line",
			raw:  "Review the parser.\nPreserve quoted separators in src/parse/input.go.\nReturn empty values unchanged.",
			want: []string{"preserve", "quoted", "separators", "empty", "unchanged"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := tc.raw + "\nhttps://example.com/irrelevant/review.py\n<execution-policy>routing-only policy-marker</execution-policy>"
			got := buildRetrievalQuery(raw)
			if got.Strategy != "structured" {
				t.Fatalf("strategy = %q, want structured", got.Strategy)
			}
			tokens := bm25.Tokenize(got.Text)
			for _, want := range tc.want {
				if !containsString(tokens, want) {
					t.Errorf("query lost task term %q: %q", want, got.Text)
				}
			}
			for _, noise := range []string{"routing", "policy", "marker", "example", "irrelevant"} {
				if containsString(tokens, noise) {
					t.Errorf("query leaked framing term %q: %q", noise, got.Text)
				}
			}
		})
	}
}

func TestBuildRetrievalQueryTaskBoundaries(t *testing.T) {
	const body = "Expired-entry parser bug in src/cache/parser.py.\nExpected: preserve escaped separators.\nActual: silently drops quoted values.\nRequirements:\nKeep empty values unchanged."
	const checkout = "You are working in a git checkout of the org/repo repository at commit abc. Resolve the GitHub issue below.\n\n"
	const requirements = "\n\nRequirements:\n- Find the root cause and implement a complete fix by editing non-test source files.\n- Leave all changes uncommitted in the working tree."
	const testbed = "Fix the following issue in /testbed. The matching Python environment and dependencies are preinstalled at /opt/miniconda3/envs/testbed; use that Python and the existing tests.\n\n"
	want := buildRetrievalQuery(body)
	for name, raw := range map[string]string{
		"plain":            body,
		"xml":              checkout + "<issue>\n" + body + "\n</issue>" + requirements,
		"labeled":          "Issue:\n" + body,
		"labeled_checkout": checkout + "Issue:\n" + body + requirements,
		"actual_testbed":   testbed + body,
		"windows_lines":    strings.ReplaceAll(testbed+body, "\n", "\r\n"),
	} {
		t.Run(name, func(t *testing.T) {
			got := buildRetrievalQuery(raw + "\n<execution-policy>routing-only policy-marker</execution-policy>")
			if got.Text != want.Text || got.Intent != want.Intent || !reflect.DeepEqual(got.Paths, want.Paths) {
				t.Fatalf("task projection = %+v, want text=%q intent=%q paths=%v", got, want.Text, want.Intent, want.Paths)
			}
			if cleaned := cleanRetrievalQuery(raw); cleaned != cleanRetrievalQuery(body) {
				t.Fatalf("clean/build task boundaries differ: %q", cleaned)
			}
			for _, word := range []string{"preserve", "escaped", "silently", "quoted", "empty", "unchanged"} {
				if !containsString(bm25.Tokenize(got.Text), word) {
					t.Errorf("lost task requirement %q: %q", word, got.Text)
				}
			}
		})
	}
}

func TestBuildRetrievalQueryKeepsEmbeddedIssueLabel(t *testing.T) {
	raw := "Explain how these headings are parsed in src/parser.go.\nIssue:\nExpected: keep the header as data."
	got := buildRetrievalQuery(raw)
	if !containsString(bm25.Tokenize(got.Text), "headings") || got.Intent != firstNonEmptyLine(raw) {
		t.Fatalf("ordinary task was mistaken for runner framing: %+v", got)
	}
}

func TestBuildRetrievalQueryIgnoresFalseStructuredSignals(t *testing.T) {
	got := buildRetrievalQuery("Investigate pre-fix expired-entry behavior with a test in the testbed.\nhttps://example.com/noise/source.py")
	if len(got.ErrorCodes) != 0 || len(got.TestNames) != 0 || len(got.Paths) != 0 || got.Strategy != "lexical_fallback" {
		t.Fatalf("ordinary prose became structured evidence: %+v", got)
	}
	if containsString(bm25.Tokenize(got.Text), "example") || containsString(bm25.Tokenize(got.Text), "noise") {
		t.Fatalf("URL leaked into fallback: %q", got.Text)
	}
}
