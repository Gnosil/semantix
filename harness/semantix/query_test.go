package semantix

import (
	"reflect"
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
