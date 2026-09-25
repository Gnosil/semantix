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
	for _, noise := range []string{"acknowledged", "assign", "ticket", "requirements", "full", "plan"} {
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

// Issue #447 offline replay (2026-09-07): the swe_pilot prompt shell has no
// <issue> tag, so the "intent" became the instruction sentence, "pre-fix"
// matched as an error code and the bare word "test" as a test name — the
// structured query carried template tokens instead of issue signals.
const labeledIssuePrompt = `You are solving a real GitHub issue in this repository (your cwd is the repo root at the pre-fix commit).

Issue:
dateformat.y() doesn't support years < 1000.
When using the dateformat of django with a value of year < 1000, the four-digit
year is not zero-padded; see django.utils.dateformat.DateFormat.y.

Instructions:
- Locate the code responsible and implement the minimal, correct fix.
- Do NOT modify test files.
- You may run quick syntax checks.
- When the fix is complete, stop.`

func TestBuildRetrievalQueryUsesLabeledIssueSectionAsIntent(t *testing.T) {
	got := buildRetrievalQuery(labeledIssuePrompt)
	if got.Strategy != "structured" {
		t.Fatalf("strategy = %q fallback = %q", got.Strategy, got.FallbackReason)
	}
	if got.Intent != "dateformat.y() doesn't support years < 1000." {
		t.Fatalf("intent = %q", got.Intent)
	}
	tokens := bm25.Tokenize(got.Text)
	for _, shell := range []string{"solving", "cwd", "root", "instructions", "modify", "syntax", "stop"} {
		if containsString(tokens, shell) {
			t.Errorf("structured query leaked prompt shell %q: %q", shell, got.Text)
		}
	}
	for _, want := range []string{"dateformat", "y", "1000"} {
		if !containsString(tokens, want) {
			t.Errorf("structured query lost issue token %q: %q", want, got.Text)
		}
	}
}

func TestCleanRetrievalQueryUsesLabeledIssueSection(t *testing.T) {
	tokens := bm25.Tokenize(cleanRetrievalQuery(labeledIssuePrompt))
	for _, shell := range []string{"solving", "cwd", "instructions", "modify", "syntax"} {
		if containsString(tokens, shell) {
			t.Errorf("cleaned query leaked prompt shell %q: %v", shell, tokens)
		}
	}
	if !containsString(tokens, "dateformat") {
		t.Errorf("cleaned query lost issue token: %v", tokens)
	}
}

func TestBuildRetrievalQueryIgnoresHyphenatedProseAsErrorCodes(t *testing.T) {
	got := buildRetrievalQuery(`<issue>
The pre-fix behaviour is read-only; a four-digit year raises UnboundLocalError
and the API returns HTTP_404 for FOO-BAR_1 codes.
</issue>`)
	for _, code := range []string{"unboundlocalerror", "http_404", "foo-bar_1"} {
		if !containsString(got.ErrorCodes, code) {
			t.Errorf("error codes = %v, missing %q", got.ErrorCodes, code)
		}
	}
	for _, prose := range []string{"pre-fix", "read-only", "four-digit"} {
		if containsString(got.ErrorCodes, prose) {
			t.Errorf("error codes = %v, must not contain prose %q", got.ErrorCodes, prose)
		}
	}
}

func TestBuildRetrievalQueryIgnoresBareTestWords(t *testing.T) {
	got := buildRetrievalQuery(`<issue>
The test suite and all tests fail while testing test_redis_timeout and TestCacheInvalidation.
</issue>`)
	if !reflect.DeepEqual(got.TestNames, []string{"test_redis_timeout", "testcacheinvalidation"}) {
		t.Fatalf("test names = %v", got.TestNames)
	}
}

func TestBuildRetrievalQueryFallsBackToIssueSectionWhenNoSignals(t *testing.T) {
	got := buildRetrievalQuery(`You are solving a real GitHub issue in this repository (your cwd is the repo root at the pre-fix commit).

Issue:
Wrong ordering of polynomial terms in the printer.

Instructions:
- Do NOT modify test files.`)
	if got.Strategy != "lexical_fallback" || got.FallbackReason != "no_structured_signals" {
		t.Fatalf("strategy = %q fallback = %q", got.Strategy, got.FallbackReason)
	}
	tokens := bm25.Tokenize(got.Text)
	for _, shell := range []string{"solving", "cwd", "instructions", "modify", "test"} {
		if containsString(tokens, shell) {
			t.Errorf("fallback query leaked prompt shell %q: %v", shell, tokens)
		}
	}
	if !containsString(tokens, "polynomial") {
		t.Errorf("fallback query lost issue token: %v", tokens)
	}
}
