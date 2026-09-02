package semantix

import (
	"strings"

	"semantix/kernel/bm25"
)

var retrievalStopwords = map[string]struct{}{
	"a": {}, "at": {}, "below": {}, "checkout": {}, "commit": {},
	"git": {}, "github": {}, "in": {}, "issue": {}, "of": {},
	"please": {}, "repository": {}, "requirements": {}, "resolve": {},
	"task": {}, "the": {}, "working": {}, "you": {}, "are": {},
}

// cleanRetrievalQuery removes host/benchmark framing before lexical search.
// It intentionally returns a token projection rather than natural prose: BM25
// and QueryCoverage consume the same tokenizer, so punctuation loss cannot
// change the lexical evidence while boilerplate cannot dominate it.
func cleanRetrievalQuery(raw string) string {
	query := stripTaggedBlock(raw, "execution-policy")
	if issue, ok := taggedBody(query, "issue"); ok {
		query = issue
	}
	tokens := bm25.Tokenize(query)
	kept := tokens[:0]
	for _, token := range tokens {
		if _, drop := retrievalStopwords[token]; !drop {
			kept = append(kept, token)
		}
	}
	return strings.Join(kept, " ")
}

func stripTaggedBlock(text, tag string) string {
	for {
		lower := strings.ToLower(text)
		start := strings.Index(lower, "<"+tag)
		if start < 0 {
			return text
		}
		openEndRel := strings.Index(lower[start:], ">")
		if openEndRel < 0 {
			return text[:start]
		}
		closeStartRel := strings.Index(lower[start+openEndRel+1:], "</"+tag+">")
		if closeStartRel < 0 {
			return text[:start]
		}
		end := start + openEndRel + 1 + closeStartRel + len(tag) + 3
		text = text[:start] + " " + text[end:]
	}
}

func taggedBody(text, tag string) (string, bool) {
	lower := strings.ToLower(text)
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	start := strings.Index(lower, open)
	if start < 0 {
		return "", false
	}
	start += len(open)
	endRel := strings.Index(lower[start:], close)
	if endRel < 0 {
		return "", false
	}
	return text[start : start+endRel], true
}
