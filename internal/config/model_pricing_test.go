package config

import (
	"encoding/json"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"gopkg.in/yaml.v3"
)

func TestModelPricingConfigDecoding(t *testing.T) {
	const yamlConfig = `codex-api-key:
  - models:
      - name: codex-upstream
        alias: codex-alias
        pricing:
          input: 1.25
          output: 10
          cached: 0.125
          cache-creation: 1.5625
          reasoning: 10
          long-context:
            threshold: 272000
            input: 2.5
            output: 15
            cached: 0.25
            cache-creation: 3.125
            reasoning: 15
claude-api-key:
  - models:
      - name: claude-upstream
        alias: claude-alias
        pricing:
          input: 3
          output: 15
gemini-api-key:
  - models:
      - name: gemini-upstream
        alias: gemini-alias
        pricing:
          input: 3
          output: 15
interactions-api-key:
  - models:
      - name: interactions-upstream
        alias: interactions-alias
        pricing:
          input: 3
          output: 15
xai-api-key:
  - models:
      - name: xai-upstream
        alias: xai-alias
        pricing:
          input: 3
          output: 15
vertex-api-key:
  - models:
      - name: vertex-upstream
        alias: vertex-alias
        pricing:
          input: 3
          output: 15
openai-compatibility:
  - models:
      - name: compat-upstream
        alias: compat-alias
        pricing:
          input: 3
          output: 15
`
	const jsonConfig = `{"codex-api-key":[{"models":[{"name":"codex-upstream","alias":"codex-alias","pricing":{"input":1.25,"output":10,"cached":0.125,"cache_creation":1.5625,"reasoning":10,"long_context":{"threshold":272000,"input":2.5,"output":15,"cached":0.25,"cache_creation":3.125,"reasoning":15}}}]}],"claude-api-key":[{"models":[{"name":"claude-upstream","alias":"claude-alias","pricing":{"input":3,"output":15}}]}],"gemini-api-key":[{"models":[{"name":"gemini-upstream","alias":"gemini-alias","pricing":{"input":3,"output":15}}]}],"interactions-api-key":[{"models":[{"name":"interactions-upstream","alias":"interactions-alias","pricing":{"input":3,"output":15}}]}],"xai-api-key":[{"models":[{"name":"xai-upstream","alias":"xai-alias","pricing":{"input":3,"output":15}}]}],"vertex-api-key":[{"models":[{"name":"vertex-upstream","alias":"vertex-alias","pricing":{"input":3,"output":15}}]}],"openai-compatibility":[{"models":[{"name":"compat-upstream","alias":"compat-alias","pricing":{"input":3,"output":15}}]}]}`

	for _, testCase := range []struct {
		name   string
		decode func(*Config) error
	}{
		{
			name: "YAML",
			decode: func(cfg *Config) error {
				return yaml.Unmarshal([]byte(yamlConfig), cfg)
			},
		},
		{
			name: "JSON",
			decode: func(cfg *Config) error {
				return json.Unmarshal([]byte(jsonConfig), cfg)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var cfg Config
			if errDecode := testCase.decode(&cfg); errDecode != nil {
				t.Fatalf("decode config: %v", errDecode)
			}

			codexPricing := cfg.CodexKey[0].Models[0].Pricing
			if codexPricing == nil {
				t.Fatal("codex pricing is nil")
			}
			if codexPricing.Input != 1.25 || codexPricing.Output != 10 {
				t.Errorf("codex pricing = %+v, want input 1.25 output 10", *codexPricing)
			}
			if codexPricing.Cached != 0.125 || codexPricing.CacheCreation != 1.5625 || codexPricing.Reasoning != 10 {
				t.Errorf("codex pricing extras = %+v", *codexPricing)
			}
			if codexPricing.LongContext == nil {
				t.Fatal("codex long-context pricing is nil")
			}
			long := codexPricing.LongContext
			if long.Threshold != 272000 || long.Input != 2.5 || long.Output != 15 {
				t.Errorf("codex long-context pricing = %+v", *long)
			}
			if long.Cached != 0.25 || long.CacheCreation != 3.125 || long.Reasoning != 15 {
				t.Errorf("codex long-context extras = %+v", *long)
			}

			others := []struct {
				name    string
				pricing *registry.ModelPricing
			}{
				{name: "claude", pricing: cfg.ClaudeKey[0].Models[0].Pricing},
				{name: "gemini", pricing: cfg.GeminiKey[0].Models[0].Pricing},
				{name: "interactions", pricing: cfg.InteractionsKey[0].Models[0].Pricing},
				{name: "xai", pricing: cfg.XAIKey[0].Models[0].Pricing},
				{name: "vertex", pricing: cfg.VertexCompatAPIKey[0].Models[0].Pricing},
				{name: "openai compatibility", pricing: cfg.OpenAICompatibility[0].Models[0].Pricing},
			}
			for _, model := range others {
				if model.pricing == nil {
					t.Errorf("%s pricing is nil", model.name)
					continue
				}
				if model.pricing.Input != 3 || model.pricing.Output != 15 {
					t.Errorf("%s pricing = %+v, want input 3 output 15", model.name, *model.pricing)
				}
			}
		})
	}
}

func TestModelPricingYAMLRoundTrip(t *testing.T) {
	model := ClaudeModel{
		Name:  "claude-upstream",
		Alias: "claude-alias",
		Pricing: &registry.ModelPricing{
			Input:         3,
			Output:        15,
			CacheCreation: 3.75,
			LongContext: &registry.LongContextPricing{
				Threshold: 200000,
				Input:     6,
				Output:    22.5,
			},
		},
	}
	data, err := yaml.Marshal(model)
	if err != nil {
		t.Fatalf("marshal model: %v", err)
	}
	var decoded ClaudeModel
	if err = yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal model: %v", err)
	}
	if decoded.Pricing == nil || decoded.Pricing.LongContext == nil {
		t.Fatalf("pricing lost in round trip: %s", string(data))
	}
	if decoded.Pricing.CacheCreation != 3.75 {
		t.Errorf("cache-creation = %v, want 3.75", decoded.Pricing.CacheCreation)
	}
	if decoded.Pricing.LongContext.Threshold != 200000 || decoded.Pricing.LongContext.Output != 22.5 {
		t.Errorf("long-context = %+v", *decoded.Pricing.LongContext)
	}
}
