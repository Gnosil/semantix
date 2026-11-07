package zone

import (
	"math"
	"testing"

	"semantix/kernel/slice"
)

func TestClassifyHitCoverage(t *testing.T) {
	for _, tc := range []struct {
		name     string
		coverage float64
		want     Zone
	}{
		{"unrelated top1", 0.2, Miss},
		{"partial top1", 0.6, Grey},
		{"grey boundary", 0.55, Grey},
		{"hit boundary", 0.8, Hit},
		{"complete", 1, Hit},
		{"zero", 0, Miss},
		{"negative", -1, Miss},
		{"out of range", 1.1, Miss},
		{"nan", math.NaN(), Miss},
		{"infinity", math.Inf(1), Miss},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hit := slice.Hit{Score: 17.837, QueryCoverage: tc.coverage, QueryCoverageValid: true}
			if got := Default().ClassifyHit(hit, hit.Score); got != tc.want {
				t.Fatalf("ClassifyHit = %v, want %v", got, tc.want)
			}
		})
	}
	hit := slice.Hit{Score: 17.837, QueryCoverage: 1, QueryCoverageValid: true}
	z := Default()
	z.AbsHigh, z.AbsLow = 25, 12
	if got := z.ClassifyHit(hit, hit.Score); got != Grey {
		t.Fatalf("calibrated absolute floor = %v, want Grey", got)
	}
	if got := Default().ClassifyHit(hit, 100); got != Miss {
		t.Fatalf("weak relative score = %v, want Miss", got)
	}
	z = Default()
	z.AbsHigh, z.AbsLow = 0, 0
	hit.QueryCoverage = 0.1
	if got := z.ClassifyHit(hit, hit.Score); got != Hit {
		t.Fatalf("explicit relative-only mode = %v, want Hit", got)
	}
	hit.QueryCoverageValid = false
	if got := Default().ClassifyHit(hit, hit.Score); got != Hit {
		t.Fatalf("unmeasured coverage = %v, want legacy Hit", got)
	}
}
