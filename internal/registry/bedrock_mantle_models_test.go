package registry

import (
	"testing"
)

func TestLookupStaticBedrockMantleModelInfo(t *testing.T) {
	tests := []struct {
		modelID             string
		wantID              string
		wantContextLength   int
		wantMaxOutput       int
		wantReasoning       bool
		wantPricingInput    float64
		wantPricingOutput   float64
		wantLongThreshold   int
		wantLongInput       float64
	}{
		{
			modelID:           "openai.gpt-5.6-luna",
			wantID:            "openai.gpt-5.6-luna",
			wantContextLength: 1000000,
			wantMaxOutput:     128000,
			wantReasoning:     true,
			wantPricingInput:  0.22,
			wantPricingOutput: 1.32,
			wantLongThreshold: 272000,
			wantLongInput:     0.44,
		},
		{
			modelID:           "gpt-5.6-luna",
			wantID:            "openai.gpt-5.6-luna",
			wantContextLength: 1000000,
			wantMaxOutput:     128000,
			wantReasoning:     true,
			wantPricingInput:  0.22,
			wantPricingOutput: 1.32,
			wantLongThreshold: 272000,
			wantLongInput:     0.44,
		},
		{
			modelID:           "openai.gpt-5.6-sol",
			wantID:            "openai.gpt-5.6-sol",
			wantContextLength: 1000000,
			wantMaxOutput:     128000,
			wantReasoning:     true,
			wantPricingInput:  4.40,
			wantPricingOutput: 22.00,
			wantLongThreshold: 272000,
			wantLongInput:     8.80,
		},
		{
			modelID:           "openai.gpt-6-astra",
			wantID:            "openai.gpt-6-astra",
			wantContextLength: 1050000,
			wantMaxOutput:     128000,
			wantReasoning:     true,
			wantPricingInput:  10.00,
			wantPricingOutput: 50.00,
			wantLongThreshold: 272000,
			wantLongInput:     20.00,
		},
		{
			modelID:           "openai.gpt-5.4",
			wantID:            "openai.gpt-5.4",
			wantContextLength: 272000,
			wantMaxOutput:     128000,
			wantReasoning:     true,
			wantPricingInput:  2.75,
			wantPricingOutput: 16.50,
			wantLongThreshold: 0,
		},
		{
			modelID:           "openai.gpt-oss-120b",
			wantID:            "openai.gpt-oss-120b",
			wantContextLength: 128000,
			wantMaxOutput:     16000,
			wantReasoning:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			info := LookupStaticBedrockMantleModelInfo(tt.modelID)
			if info == nil {
				t.Fatalf("LookupStaticBedrockMantleModelInfo(%q) returned nil", tt.modelID)
			}
			if info.ContextLength != tt.wantContextLength {
				t.Errorf("ContextLength = %d, want %d", info.ContextLength, tt.wantContextLength)
			}
			if info.MaxCompletionTokens != tt.wantMaxOutput {
				t.Errorf("MaxCompletionTokens = %d, want %d", info.MaxCompletionTokens, tt.wantMaxOutput)
			}
			if (info.Thinking != nil) != tt.wantReasoning {
				t.Errorf("reasoning = %v, want %v", info.Thinking != nil, tt.wantReasoning)
			}
			if tt.wantPricingInput > 0 {
				if info.Pricing == nil {
					t.Fatal("expected Pricing to be non-nil")
				}
				if info.Pricing.Input != tt.wantPricingInput {
					t.Errorf("Pricing.Input = %f, want %f", info.Pricing.Input, tt.wantPricingInput)
				}
				if info.Pricing.Output != tt.wantPricingOutput {
					t.Errorf("Pricing.Output = %f, want %f", info.Pricing.Output, tt.wantPricingOutput)
				}
				if tt.wantLongThreshold > 0 {
					if info.Pricing.LongContext == nil {
						t.Fatal("expected Pricing.LongContext to be non-nil")
					}
					if info.Pricing.LongContext.Threshold != tt.wantLongThreshold {
						t.Errorf("LongContext.Threshold = %d, want %d", info.Pricing.LongContext.Threshold, tt.wantLongThreshold)
					}
					if info.Pricing.LongContext.Input != tt.wantLongInput {
						t.Errorf("LongContext.Input = %f, want %f", info.Pricing.LongContext.Input, tt.wantLongInput)
					}
				}
			}
		})
	}
}

func TestLookupStaticModelInfoResolvesBedrockMantle(t *testing.T) {
	info := LookupStaticModelInfo("openai.gpt-5.6-luna")
	if info == nil {
		t.Fatal("LookupStaticModelInfo(openai.gpt-5.6-luna) = nil, want model")
	}
	if info.ContextLength != 1000000 {
		t.Errorf("ContextLength = %d, want 1000000", info.ContextLength)
	}

	infoUnprefixed := LookupStaticBedrockMantleModelInfo("gpt-5.6-luna")
	if infoUnprefixed == nil {
		t.Fatal("LookupStaticBedrockMantleModelInfo(gpt-5.6-luna) = nil, want model")
	}
	if infoUnprefixed.ContextLength != 1000000 {
		t.Errorf("ContextLength = %d, want 1000000", infoUnprefixed.ContextLength)
	}
}

func TestConvertModelToMapOpenAIRichMetadata(t *testing.T) {
	r := newTestModelRegistry()
	luna := LookupStaticBedrockMantleModelInfo("openai.gpt-5.6-luna")
	if luna == nil {
		t.Fatal("missing luna static model")
	}

	mapped := r.convertModelToMap(luna, "openai")
	if mapped == nil {
		t.Fatal("convertModelToMap returned nil")
	}

	if mapped["id"] != "openai.gpt-5.6-luna" {
		t.Errorf("id = %v, want openai.gpt-5.6-luna", mapped["id"])
	}
	if mapped["context_length"] != 1000000 {
		t.Errorf("context_length = %v, want 1000000", mapped["context_length"])
	}
	if mapped["max_completion_tokens"] != 128000 {
		t.Errorf("max_completion_tokens = %v, want 128000", mapped["max_completion_tokens"])
	}
	if mapped["reasoning"] != true {
		t.Errorf("reasoning = %v, want true", mapped["reasoning"])
	}

	capabilities, ok := mapped["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities is not a map: %#v", mapped["capabilities"])
	}
	if capabilities["contextWindow"] != 1000000 {
		t.Errorf("capabilities.contextWindow = %v, want 1000000", capabilities["contextWindow"])
	}
	if capabilities["maxOutput"] != 128000 {
		t.Errorf("capabilities.maxOutput = %v, want 128000", capabilities["maxOutput"])
	}
	if capabilities["reasoning"] != true {
		t.Errorf("capabilities.reasoning = %v, want true", capabilities["reasoning"])
	}
	if capabilities["vision"] != true {
		t.Errorf("capabilities.vision = %v, want true", capabilities["vision"])
	}

	pricing, ok := mapped["pricing"].(map[string]any)
	if !ok {
		t.Fatalf("pricing is not a map: %#v", mapped["pricing"])
	}
	if pricing["input"] != 0.22 {
		t.Errorf("pricing.input = %v, want 0.22", pricing["input"])
	}
	if pricing["output"] != 1.32 {
		t.Errorf("pricing.output = %v, want 1.32", pricing["output"])
	}

	compat, ok := mapped["compat"].(map[string]any)
	if !ok {
		t.Fatalf("compat is not a map: %#v", mapped["compat"])
	}
	if compat["api"] != "openai-responses" {
		t.Errorf("compat.api = %v, want openai-responses", compat["api"])
	}
}
