package main

import (
	"fmt"
	"io"
	"os"

	"semantix/kernel/embed"
	"semantix/kernel/slice"
)

// embedBatch bounds one remote embeddings call (keeps responses well under
// the client's read limit).
const embedBatch = 64

// embedItems computes vectors for extracted slices in batches of embedBatch
// and stores them in Slice.Embedding, recording the embedder provenance
// (model + dimension) in Slice.Meta. A remote failure inside ModelEmbedder
// degrades to hash vectors (fail-soft), so this only errors when the
// fallback itself fails. A batch that fell back is recorded as "hash", not
// as the remote model (Issue #512).
//
// Hash vectors are not persisted at all: they are deterministic functions of
// Content (recomputable any time — search's vector/hybrid retrievers already
// re-embed per query and never read the stored field), so writing them only
// inflated every library line with 256 floats nothing ever read.
func embedItems(emb embed.Embedder, items []*slice.Slice, kind string) error {
	if kind == "hash" {
		return nil
	}
	for start := 0; start < len(items); start += embedBatch {
		end := min(start+embedBatch, len(items))
		texts := make([]string, 0, end-start)
		for _, item := range items[start:end] {
			texts = append(texts, string(item.Content))
		}
		vecs, label, err := embedTexts(emb, texts, kind)
		if err != nil {
			return err
		}
		for j, v := range vecs {
			items[start+j].Embedding = append([]float32(nil), v...)
			items[start+j].Meta.EmbedModel = label
			items[start+j].Meta.EmbedDim = len(v)
		}
	}
	return nil
}

// embedTexts embeds one batch and returns the label naming who produced the
// vectors: the remote model, or "hash" when the remote call fell back.
func embedTexts(emb embed.Embedder, texts []string, kind string) ([][]float32, string, error) {
	remote, ok := emb.(*embed.ModelEmbedder)
	if !ok {
		vecs, err := emb.Embed(texts)
		return vecs, kind, err
	}
	vecs, fellBack, err := remote.EmbedReport(texts)
	if fellBack {
		return vecs, "hash", err
	}
	return vecs, remote.Model(), err
}

// buildEmbedder constructs the CLI embedder from the --embedder flag.
// hash keeps the zero-dependency default; model wires a remote
// OpenAI-compatible embeddings API from the environment
// (SEMANTIX_EMBED_BASE_URL / SEMANTIX_EMBED_MODEL / SEMANTIX_EMBED_API_KEY).
// Remote failures degrade to hash inside ModelEmbedder (fail-soft);
// OnFallback surfaces a one-line warning on the CLI stderr.
func buildEmbedder(kind string, warn io.Writer) (embed.Embedder, error) {
	switch kind {
	case "hash":
		return embed.HashEmbedder{}, nil
	case "model":
		cfg := embed.ModelEmbedderConfig{
			BaseURL: os.Getenv("SEMANTIX_EMBED_BASE_URL"),
			Model:   os.Getenv("SEMANTIX_EMBED_MODEL"),
			APIKey:  os.Getenv("SEMANTIX_EMBED_API_KEY"),
			OnFallback: func(err error) {
				fmt.Fprintf(warn, "semantix: embed API unavailable (%v); fell back to hash embedding\n", err)
			},
		}
		return embed.NewModelEmbedder(cfg)
	default:
		return nil, fmt.Errorf("invalid --embedder %q (want hash or model)", kind)
	}
}
