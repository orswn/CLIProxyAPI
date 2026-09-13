package registry

import "testing"

func TestNormalizeModelPricingDropsInvalidValues(t *testing.T) {
	if got := NormalizeModelPricing(nil); got != nil {
		t.Fatalf("nil pricing = %+v, want nil", got)
	}
	if got := NormalizeModelPricing(&ModelPricing{}); got != nil {
		t.Fatalf("empty pricing = %+v, want nil", got)
	}

	normalized := NormalizeModelPricing(&ModelPricing{
		Input:  -1,
		Output: 15,
		LongContext: &LongContextPricing{
			Threshold: 0,
			Input:     6,
		},
	})
	if normalized == nil {
		t.Fatal("normalized pricing is nil")
	}
	if normalized.Input != 0 {
		t.Errorf("input = %v, want 0", normalized.Input)
	}
	if normalized.Output != 15 {
		t.Errorf("output = %v, want 15", normalized.Output)
	}
	if normalized.LongContext != nil {
		t.Errorf("long context = %+v, want nil for non-positive threshold", *normalized.LongContext)
	}

	zeroRates := NormalizeModelPricing(&ModelPricing{
		Input:       3,
		LongContext: &LongContextPricing{Threshold: 200000},
	})
	if zeroRates == nil {
		t.Fatal("pricing with input only is nil")
	}
	if zeroRates.LongContext != nil {
		t.Errorf("long context = %+v, want nil when every rate is zero", *zeroRates.LongContext)
	}
}

func TestNormalizeModelPricingCopiesInput(t *testing.T) {
	source := &ModelPricing{
		Input:       3,
		Output:      15,
		LongContext: &LongContextPricing{Threshold: 200000, Input: 6, Output: 22.5},
	}
	normalized := NormalizeModelPricing(source)
	if normalized == source {
		t.Fatal("normalized pricing aliases the source pointer")
	}
	if normalized.LongContext == source.LongContext {
		t.Fatal("normalized long context aliases the source pointer")
	}
	normalized.Input = 99
	normalized.LongContext.Input = 99
	if source.Input != 3 || source.LongContext.Input != 6 {
		t.Errorf("source mutated: %+v", *source)
	}
}

func TestModelPricingClone(t *testing.T) {
	var nilPricing *ModelPricing
	if got := nilPricing.Clone(); got != nil {
		t.Fatalf("nil clone = %+v, want nil", got)
	}
	source := &ModelPricing{Input: 3, LongContext: &LongContextPricing{Threshold: 200000, Input: 6}}
	clone := source.Clone()
	clone.LongContext.Input = 99
	if source.LongContext.Input != 6 {
		t.Errorf("clone shares long context with source: %+v", *source.LongContext)
	}
}
