package config

import "testing"

func TestSanitizeCombos(t *testing.T) {
	cfg := &Config{SDKConfig: SDKConfig{Combos: []ComboConfig{
		{Name: "  Coding  ", Models: []string{" gpt-5.4 ", "gpt-5.4", " claude-sonnet-4-6 "}},
		{Name: "coding", Models: []string{"ignored-model"}},
		{Name: "", Models: []string{"ignored"}},
		{Name: "Round", Strategy: "ROUND-ROBIN", StickyRoundRobinLimit: 0, Models: []string{"a"}},
	}}}
	cfg.SanitizeCombos()
	if len(cfg.Combos) != 2 {
		t.Fatalf("got %d combos, want 2", len(cfg.Combos))
	}
	if cfg.Combos[0].Name != "Coding" || len(cfg.Combos[0].Models) != 2 {
		t.Fatalf("unexpected first combo: %+v", cfg.Combos[0])
	}
	if cfg.Combos[0].Strategy != ComboStrategyFallback || cfg.Combos[0].StickyRoundRobinLimit != 1 {
		t.Fatalf("unexpected fallback defaults: %+v", cfg.Combos[0])
	}
	if cfg.Combos[1].Strategy != ComboStrategyRoundRobin || cfg.Combos[1].StickyRoundRobinLimit != 1 {
		t.Fatalf("unexpected round-robin defaults: %+v", cfg.Combos[1])
	}
}

func TestValidateCombos(t *testing.T) {
	valid := []ComboConfig{
		{Name: "valid-fallback", Models: []string{"gpt-5.4", "claude-sonnet-4-6"}, Strategy: ComboStrategyFallback},
		{Name: "valid-rr", Models: []string{"model-a", "model-b"}, Strategy: ComboStrategyRoundRobin},
		{Name: "valid-fusion", Models: []string{"model-a", "model-b"}, Strategy: ComboStrategyFusion, JudgeModel: "judge-model"},
	}
	if err := ValidateCombos(valid); err != nil {
		t.Fatalf("ValidateCombos unexpected error: %v", err)
	}

	nested := []ComboConfig{
		{Name: "base-combo", Models: []string{"model-a"}},
		{Name: "nested-combo", Models: []string{"base-combo"}},
	}
	if err := ValidateCombos(nested); err == nil {
		t.Fatalf("ValidateCombos expected error on nested combo, got nil")
	}

	unsupported := []ComboConfig{
		{Name: "bad-strategy", Models: []string{"model-a"}, Strategy: "invalid-strat"},
	}
	if err := ValidateCombos(unsupported); err == nil {
		t.Fatalf("ValidateCombos expected error on unsupported strategy, got nil")
	}
}
