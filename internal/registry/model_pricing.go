package registry

// Clone returns a deep copy of the pricing definition.
func (p *ModelPricing) Clone() *ModelPricing {
	if p == nil {
		return nil
	}
	out := *p
	if p.LongContext != nil {
		longContext := *p.LongContext
		out.LongContext = &longContext
	}
	return &out
}

// NormalizeModelPricing returns a sanitized deep copy of pricing.
// Negative rates are dropped, long-context pricing without a positive threshold
// is discarded, and a definition without any usable rate returns nil.
func NormalizeModelPricing(pricing *ModelPricing) *ModelPricing {
	if pricing == nil {
		return nil
	}
	out := &ModelPricing{
		Input:         nonNegativeRate(pricing.Input),
		Output:        nonNegativeRate(pricing.Output),
		Cached:        nonNegativeRate(pricing.Cached),
		CacheCreation: nonNegativeRate(pricing.CacheCreation),
		Reasoning:     nonNegativeRate(pricing.Reasoning),
	}
	if long := pricing.LongContext; long != nil && long.Threshold > 0 {
		normalizedLong := &LongContextPricing{
			Threshold:     long.Threshold,
			Input:         nonNegativeRate(long.Input),
			Output:        nonNegativeRate(long.Output),
			Cached:        nonNegativeRate(long.Cached),
			CacheCreation: nonNegativeRate(long.CacheCreation),
			Reasoning:     nonNegativeRate(long.Reasoning),
		}
		if normalizedLong.Input > 0 || normalizedLong.Output > 0 || normalizedLong.Cached > 0 || normalizedLong.CacheCreation > 0 || normalizedLong.Reasoning > 0 {
			out.LongContext = normalizedLong
		}
	}
	if out.Input == 0 && out.Output == 0 && out.Cached == 0 && out.CacheCreation == 0 && out.Reasoning == 0 && out.LongContext == nil {
		return nil
	}
	return out
}

func nonNegativeRate(value float64) float64 {
	if value < 0 {
		return 0
	}
	return value
}
