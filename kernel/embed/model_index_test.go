package embed

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestModelEmbedderResponseIndices(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
		fallback bool
	}{
		{"ordered", `{"data":[{"index":0,"embedding":[3,0]},{"index":1,"embedding":[0,4]}]}`, false},
		{"reversed", `{"data":[{"index":1,"embedding":[0,4]},{"index":0,"embedding":[3,0]}]}`, false},
		{"legacy positional", `{"data":[{"embedding":[3,0]},{"embedding":[0,4]}]}`, false},
		{"duplicate", `{"data":[{"index":0,"embedding":[3,0]},{"index":0,"embedding":[0,4]}]}`, true},
		{"negative", `{"data":[{"index":-1,"embedding":[3,0]},{"index":1,"embedding":[0,4]}]}`, true},
		{"out of range", `{"data":[{"index":0,"embedding":[3,0]},{"index":2,"embedding":[0,4]}]}`, true},
		{"missing first", `{"data":[{"embedding":[3,0]},{"index":1,"embedding":[0,4]}]}`, true},
		{"missing last", `{"data":[{"index":0,"embedding":[3,0]},{"embedding":[0,4]}]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()
			var fallbackErr error
			fallback := HashEmbedder{Dim: 8}
			m, err := NewModelEmbedder(ModelEmbedderConfig{
				BaseURL: srv.URL, Model: "test-model", APIKey: "test-key", Dim: 2,
				Fallback: fallback, OnFallback: func(err error) { fallbackErr = err },
			})
			if err != nil {
				t.Fatal(err)
			}
			texts := []string{"first input", "second input"}
			got, fellBack, err := m.EmbedReport(texts)
			if err != nil {
				t.Fatal(err)
			}
			if fellBack != tc.fallback || (fallbackErr != nil) != tc.fallback {
				t.Fatalf("fallback = %v, callback error = %v, want fallback %v", fellBack, fallbackErr, tc.fallback)
			}
			want := [][]float32{{1, 0}, {0, 1}}
			if tc.fallback {
				want, err = fallback.Embed(texts)
				if err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("vectors = %v, want %v", got, want)
			}
		})
	}
}
