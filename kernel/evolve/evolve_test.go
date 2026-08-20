package evolve

import (
	"math"
	"testing"
)

func TestNewDefaults(t *testing.T) {
	e := New(Config{})
	p := e.Params()
	if p.TauL2 != DefaultTauL2 {
		t.Fatalf("TauL2 default = %v, want %v", p.TauL2, DefaultTauL2)
	}
	if p.InjectCap != DefaultInjectCap {
		t.Fatalf("InjectCap default = %v", p.InjectCap)
	}
	if p.FreezeEpochs != DefaultFreezeEpoch {
		t.Fatalf("FreezeEpochs default = %v", p.FreezeEpochs)
	}
}

func TestRecordSignalEWMA(t *testing.T) {
	e := New(Config{Alpha: 0.5})
	// First hit signal at epoch 1: EWMA starts at 0 → 0.5*1 + 0.5*0 = 0.5
	if err := e.RecordSignal(Signal{Name: "cache_hit", Value: 1, Epoch: 1}); err != nil {
		t.Fatal(err)
	}
	// Second hit: 0.5*1 + 0.5*0.5 = 0.75
	if err := e.RecordSignal(Signal{Name: "cache_hit", Value: 1, Epoch: 2}); err != nil {
		t.Fatal(err)
	}
	if e.hitEWMA < 0.749 || e.hitEWMA > 0.751 {
		t.Fatalf("hitEWMA = %v, want ~0.75", e.hitEWMA)
	}
	if e.Epoch() != 2 {
		t.Fatalf("epoch = %d, want 2", e.Epoch())
	}
}

func TestRecordSignalClampsValue(t *testing.T) {
	e := New(Config{Alpha: 0.5})
	// A corrupt signal of 99 must be clamped to 1.
	if err := e.RecordSignal(Signal{Name: "cache_hit", Value: 99, Epoch: 1}); err != nil {
		t.Fatal(err)
	}
	if e.hitEWMA != 0.5 {
		t.Fatalf("hitEWMA = %v, want 0.5 (clamped)", e.hitEWMA)
	}
	// Negative must clamp to 0.
	if err := e.RecordSignal(Signal{Name: "cache_hit", Value: -5, Epoch: 2}); err != nil {
		t.Fatal(err)
	}
	if e.hitEWMA != 0.25 {
		t.Fatalf("hitEWMA = %v, want 0.25 (clamped 0)", e.hitEWMA)
	}
}

func TestRecordSignalRejectsOutOfOrder(t *testing.T) {
	e := New(Config{})
	if err := e.RecordSignal(Signal{Name: "cache_hit", Value: 1, Epoch: 5}); err != nil {
		t.Fatal(err)
	}
	if err := e.RecordSignal(Signal{Name: "cache_hit", Value: 1, Epoch: 3}); err == nil {
		t.Fatal("out-of-order epoch must be rejected")
	}
}

func TestUnknownSignalIgnored(t *testing.T) {
	e := New(Config{})
	if err := e.RecordSignal(Signal{Name: "telemetry_future", Value: 1, Epoch: 1}); err != nil {
		t.Fatal(err)
	}
	if e.hitEWMA != 0 || e.polEWMA != 0 {
		t.Fatalf("unknown signal must be ignored, got hit=%v pol=%v", e.hitEWMA, e.polEWMA)
	}
}

func TestPollutionTightensTau(t *testing.T) {
	cfg := Config{Alpha: 0.5, MinSamples: 4, FreezeEpochs: 1} // minimal freeze: adjusts from epoch 1
	e := New(cfg)
	// 4 pollution signals → polEWMA rises, tau must tighten.
	for i := uint64(1); i <= 4; i++ {
		if err := e.RecordSignal(Signal{Name: "inject_pollution", Value: 1, Epoch: i}); err != nil {
			t.Fatal(err)
		}
	}
	if e.Adjustments() == 0 {
		t.Fatal("pollution must trigger a tightening adjustment")
	}
	if got := e.Params().TauL2; got <= DefaultTauL2 {
		t.Fatalf("TauL2 = %v, want > %v after pollution (tighten = raise the floor)", got, DefaultTauL2)
	}
}

func TestHitsRelaxTau(t *testing.T) {
	cfg := Config{Alpha: 0.5, MinSamples: 4, FreezeEpochs: 1}
	e := New(cfg)
	for i := uint64(1); i <= 4; i++ {
		if err := e.RecordSignal(Signal{Name: "cache_hit", Value: 1, Epoch: i}); err != nil {
			t.Fatal(err)
		}
	}
	if e.Adjustments() == 0 {
		t.Fatal("high hit rate must trigger a relaxation adjustment")
	}
	if got := e.Params().TauL2; got >= DefaultTauL2 {
		t.Fatalf("TauL2 = %v, want < %v after hits (relax = admit more reuse)", got, DefaultTauL2)
	}
}

func TestTauClampedToBounds(t *testing.T) {
	cfg := Config{Alpha: 0.5, MinSamples: 1, FreezeEpochs: 0, MinTau: 0.30, MaxTau: 0.80}
	e := New(cfg)
	// Hammer pollution (tighten drives tau up): must never exceed MaxTau.
	for i := uint64(1); i <= 30; i++ {
		if err := e.RecordSignal(Signal{Name: "inject_pollution", Value: 1, Epoch: i}); err != nil {
			t.Fatal(err)
		}
	}
	if got := e.Params().TauL2; got > cfg.MaxTau {
		t.Fatalf("TauL2 = %v above max %v", got, cfg.MaxTau)
	}
	// Hammer hits (relax drives tau down): must never go below MinTau.
	e2 := New(cfg)
	for i := uint64(1); i <= 30; i++ {
		if err := e2.RecordSignal(Signal{Name: "cache_hit", Value: 1, Epoch: i}); err != nil {
			t.Fatal(err)
		}
	}
	if got := e2.Params().TauL2; got < cfg.MinTau {
		t.Fatalf("TauL2 = %v below min %v", got, cfg.MinTau)
	}
}

func TestFreezeWindowRejectsApply(t *testing.T) {
	cfg := Config{MinSamples: 0, FreezeEpochs: 10}
	e := New(cfg)
	// First apply installs params and opens a 10-epoch freeze.
	if err := e.Apply(Params{TauL2: 0.6, InjectCap: 0.3, FreezeEpochs: 10}); err != nil {
		t.Fatal(err)
	}
	// A change inside the window must be rejected.
	if err := e.Apply(Params{TauL2: 0.7, InjectCap: 0.3, FreezeEpochs: 10}); err == nil {
		t.Fatal("apply inside freeze window must be rejected")
	}
	// Explicit override (FreezeEpochs=0 in the applied snapshot) is allowed.
	if err := e.Apply(Params{TauL2: 0.7, InjectCap: 0.3, FreezeEpochs: 0}); err != nil {
		t.Fatalf("explicit override must be allowed: %v", err)
	}
	if got := e.Params().TauL2; got != 0.7 {
		t.Fatalf("TauL2 = %v, want 0.7 after override", got)
	}
}

func TestApplyClampsTau(t *testing.T) {
	cfg := Config{MinTau: 0.30, MaxTau: 0.80, FreezeEpochs: 0}
	e := New(cfg)
	if err := e.Apply(Params{TauL2: 9.0, InjectCap: 0.3, FreezeEpochs: 0}); err != nil {
		t.Fatal(err)
	}
	if got := e.Params().TauL2; got != 0.80 {
		t.Fatalf("TauL2 = %v, want clamped 0.80", got)
	}
}

func TestFreezeBlocksAutoAdjust(t *testing.T) {
	cfg := Config{Alpha: 0.5, MinSamples: 2, FreezeEpochs: 100}
	e := New(cfg)
	// Signals within the initial freeze (epochs < 100) must NOT adjust.
	for i := uint64(1); i <= 4; i++ {
		if err := e.RecordSignal(Signal{Name: "inject_pollution", Value: 1, Epoch: i}); err != nil {
			t.Fatal(err)
		}
	}
	if e.Adjustments() != 0 {
		t.Fatalf("adjustments = %d inside freeze, want 0", e.Adjustments())
	}
	// Past the window (epoch 200) the same signals adjust.
	for i := uint64(150); i <= 160; i++ {
		if err := e.RecordSignal(Signal{Name: "inject_pollution", Value: 1, Epoch: i}); err != nil {
			t.Fatal(err)
		}
	}
	if e.Adjustments() == 0 {
		t.Fatal("adjustments must happen after the freeze window")
	}
}

func TestRecordSignalRejectsNaN(t *testing.T) {
	e := New(Config{})
	if err := e.RecordSignal(Signal{Name: "cache_hit", Value: math.NaN(), Epoch: 1}); err == nil {
		t.Fatal("NaN signal must be rejected")
	}
	if e.hitEWMA != 0 {
		t.Fatalf("hitEWMA must stay 0 after NaN rejection, got %v", e.hitEWMA)
	}
}

func TestApplyRejectsNaN(t *testing.T) {
	e := New(Config{FreezeEpochs: 0})
	if err := e.Apply(Params{TauL2: math.NaN(), InjectCap: 0.3, FreezeEpochs: 0}); err == nil {
		t.Fatal("NaN param must be rejected")
	}
	if got := e.Params().TauL2; got != DefaultTauL2 {
		t.Fatalf("TauL2 must stay default after NaN rejection, got %v", got)
	}
}

func TestConcurrentSignals(t *testing.T) {
	e := New(Config{})
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func(g int) {
			defer func() { done <- struct{}{} }()
			for n := 0; n < 200; n++ {
				name := "cache_hit"
				if g%2 == 0 {
					name = "inject_pollution"
				}
				_ = e.RecordSignal(Signal{Name: name, Value: 1, Epoch: uint64(g*200 + n + 1)})
			}
		}(i)
	}
	for i := 0; i < 8; i++ {
		<-done
	}
	// No deadlock, no panic; state remains sane.
	if e.Adjustments() == 0 && e.sampleCount == 0 {
		t.Fatal("concurrent signals must be recorded")
	}
}

func TestConfigRejectsNonFinite(t *testing.T) {
	// NaN/±Inf config values must fall back to defaults (never poison the engine).
	e := New(Config{Alpha: math.NaN(), MinTau: math.Inf(1), MaxTau: math.Inf(-1), TauL2: math.NaN(), InjectCap: math.Inf(1)})
	if e.alpha != DefaultAlpha {
		t.Fatalf("alpha = %v, want default %v", e.alpha, DefaultAlpha)
	}
	if e.minTau != DefaultMinTau || e.maxTau != DefaultMaxTau {
		t.Fatalf("tau bounds must fall back to defaults, got [%v,%v]", e.minTau, e.maxTau)
	}
	p := e.Params()
	if p.TauL2 != DefaultTauL2 || p.InjectCap != DefaultInjectCap {
		t.Fatalf("params must fall back to defaults, got TauL2=%v InjectCap=%v", p.TauL2, p.InjectCap)
	}
}

func TestRecordSignalRejectsInf(t *testing.T) {
	e := New(Config{})
	if err := e.RecordSignal(Signal{Name: "cache_hit", Value: math.Inf(1), Epoch: 1}); err == nil {
		t.Fatal("Inf signal must be rejected")
	}
	if err := e.RecordSignal(Signal{Name: "cache_hit", Value: math.Inf(-1), Epoch: 2}); err == nil {
		t.Fatal("-Inf signal must be rejected")
	}
	if e.sampleCount != 0 {
		t.Fatalf("rejected signals must not consume state, sampleCount=%d", e.sampleCount)
	}
	if e.Epoch() != 0 {
		t.Fatalf("rejected signals must not advance epoch, got %d", e.Epoch())
	}
}

func TestApplyRejectsInf(t *testing.T) {
	e := New(Config{})
	if err := e.Apply(Params{TauL2: math.Inf(1), InjectCap: 0.3, FreezeEpochs: 0}); err == nil {
		t.Fatal("Inf param must be rejected")
	}
}

func TestParamsSnapshotIsCopy(t *testing.T) {
	e := New(Config{})
	p1 := e.Params()
	p1.TauL2 = 0.99
	if got := e.Params().TauL2; got == 0.99 {
		t.Fatal("mutating the returned snapshot must not affect the engine")
	}
}
