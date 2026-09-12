package management

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/bedrockmantle"
)

var mantleRegionPattern = regexp.MustCompile(`^[a-z]{2}(-[a-z0-9]{1,16}){1,3}-[1-9][0-9]?$`)
var mantleModelPattern = regexp.MustCompile(`^openai\.[a-zA-Z0-9][a-zA-Z0-9._-]{0,199}$`)

func validateMantleRegion(region string) error {
	if !mantleRegionPattern.MatchString(region) {
		return fmt.Errorf("invalid AWS region")
	}
	return nil
}

func parseMantleModelRegions(raw string) (map[string]string, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("model_regions must be an object")
	}
	regions := make(map[string]string)
	seen := make(map[string]bool)
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("invalid model_regions object")
		}
		model, ok := token.(string)
		if !ok || !mantleModelPattern.MatchString(model) {
			return nil, fmt.Errorf("use canonical openai.* model IDs without a credential prefix")
		}
		key := strings.ToLower(model)
		if seen[key] {
			return nil, fmt.Errorf("duplicate model region override")
		}
		seen[key] = true
		var region string
		if err = decoder.Decode(&region); err != nil {
			return nil, fmt.Errorf("model region must be a string")
		}
		region = strings.TrimSpace(region)
		if err = validateMantleRegion(region); err != nil {
			return nil, err
		}
		regions[model] = region
		if len(regions) > 256 {
			return nil, fmt.Errorf("too many model region overrides")
		}
	}
	if _, err = decoder.Token(); err != nil {
		return nil, fmt.Errorf("invalid model_regions object")
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("invalid model_regions suffix")
	}
	return regions, nil
}

func applyMantleSetupRegions(storage *bedrockmantle.Storage, raw string) error {
	if storage.DefaultRegion == "" {
		storage.DefaultRegion = bedrockmantle.DefaultBedrockRegion
	}
	if err := validateMantleRegion(storage.DefaultRegion); err != nil {
		return err
	}
	if raw != "" {
		regions, err := parseMantleModelRegions(raw)
		if err != nil {
			return err
		}
		storage.ModelRegions = regions
	}
	return nil
}

func normalizeMantleRegionPatch(fields map[string]json.RawMessage) (bool, error) {
	changed := false
	for key, raw := range fields {
		root := rootAuthFileField(key)
		if root != "model_regions" && root != "default_region" {
			continue
		}
		if root != key {
			return false, fmt.Errorf("region settings do not support dotted field paths")
		}
		if root == "model_regions" {
			regions, err := parseMantleModelRegions(string(raw))
			if err != nil {
				return false, err
			}
			fields[key], _ = json.Marshal(regions)
		} else {
			var region string
			if err := json.Unmarshal(raw, &region); err != nil {
				return false, fmt.Errorf("default_region must be a string")
			}
			region = strings.TrimSpace(region)
			if err := validateMantleRegion(region); err != nil {
				return false, err
			}
			fields[key], _ = json.Marshal(region)
		}
		changed = true
	}
	return changed, nil
}
