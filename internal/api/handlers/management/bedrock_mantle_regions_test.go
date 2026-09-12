package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/bedrockmantle"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	sdkauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestMantleRegionInputRejectedBeforeLoginOrSave(t *testing.T) {
	for _, raw := range []string{`[]`, `null`, `{"work/openai.gpt-5.6-luna":"us-east-1"}`, `{"openai.gpt-5.6-luna":""}`, `{"openai.gpt-5.6-luna":1}`, `{"openai.gpt-5.6-luna":"https://example.com"}`, `{"openai.gpt-5.6-luna":"us-east-1","openai.gpt-5.6-luna":"us-west-2"}`} {
		t.Run(raw, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				values := url.Values{"start_url": {"https://example.awsapps.com/start"}, "access_key_id": {"TEST"}, "secret_access_key": {"TEST"}, "model_regions": {raw}}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				if method == http.MethodGet {
					c.Request = httptest.NewRequest(method, "/bedrock-mantle-auth-url?"+values.Encode(), nil)
					(&Handler{}).RequestBedrockMantleToken(c)
				} else {
					c.Request = httptest.NewRequest(method, "/bedrock-mantle-key", strings.NewReader(values.Encode()))
					c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					(&Handler{}).AddBedrockMantleKey(c)
				}
				if rec.Code != 400 {
					t.Fatalf("%s invalid input accepted: %d", method, rec.Code)
				}
			}
		})
	}
}

func TestMantleStaticRegionPersistence(t *testing.T) {
	dir := t.TempDir()
	store := sdkauth.NewFileTokenStore()
	store.SetBaseDir(dir)
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: dir}, nil)
	h.tokenStore = store
	values := url.Values{"access_key_id": {"TEST"}, "secret_access_key": {"TEST"}, "region": {"us-east-1"}, "model_regions": {`{"openai.gpt-5.6-luna":"us-west-2"}`}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/bedrock-mantle-key", strings.NewReader(values.Encode()))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.AddBedrockMantleKey(c)
	if rec.Code != 200 {
		t.Fatalf("save failed: %s", rec.Body)
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatal("credential not saved")
	}
	raw, err := os.ReadFile(filepath.Join(dir, files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	if bedrockmantle.FromMetadata(metadata).ResolveRegion("openai.gpt-5.6-luna") != "us-west-2" {
		t.Fatal("override lost")
	}
}

func TestMantleRegionPatchPersistenceAndValidation(t *testing.T) {
	dir := t.TempDir()
	store := sdkauth.NewFileTokenStore()
	store.SetBaseDir(dir)
	manager := coreauth.NewManager(store, nil, nil)
	s := &bedrockmantle.Storage{AuthMode: "static", AccessKeyID: "TEST", SecretAccessKey: "SECRET", DefaultRegion: "us-east-1", ModelRegions: map[string]string{"openai.gpt-5.4": "eu-west-1"}}
	auth := &coreauth.Auth{ID: "mantle.json", FileName: "mantle.json", Provider: mantleProvider, Prefix: "work", Storage: s, Metadata: s.ToMetadata()}
	if _, err := manager.Register(coreauth.WithAuthCreationIntent(context.Background()), auth); err != nil {
		t.Fatal(err)
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: dir}, manager)
	patch := func(body string) int {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPatch, "/auth-files/fields", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		h.PatchAuthFileFields(c)
		return rec.Code
	}
	for _, body := range []string{
		`{"name":"mantle.json","default_region":"bad"}`,
		`{"name":"mantle.json","model_regions":{"work/openai.gpt-5.6-luna":"us-west-2"}}`,
		`{"name":"mantle.json","model_regions.openai.gpt-5.6-luna":"us-west-2"}`,
		`{"name":"mantle.json","default_region.value":"us-west-2"}`,
		`{"name":"mantle.json","model_regions":{"openai.gpt-5.6-luna":"us-west-2","OPENAI.GPT-5.6-LUNA":"us-east-1"}}`,
	} {
		if patch(body) != 400 {
			t.Fatalf("invalid patch accepted: %s", body)
		}
	}
	for _, regions := range []string{`{"openai.gpt-5.6-luna":"us-west-2"}`, `{}`} {
		if code := patch(`{"name":"mantle.json","default_region":"eu-central-1","model_regions":` + regions + `}`); code != 200 {
			t.Fatalf("patch: %d", code)
		}
		raw, err := os.ReadFile(filepath.Join(dir, "mantle.json"))
		if err != nil {
			t.Fatal(err)
		}
		var metadata map[string]any
		if err := json.Unmarshal(raw, &metadata); err != nil {
			t.Fatal(err)
		}
		saved := bedrockmantle.FromMetadata(metadata)
		expected := "us-west-2"
		if regions == "{}" {
			expected = "eu-central-1"
		}
		if saved.ResolveRegion("openai.gpt-5.6-luna") != expected || saved.ResolveRegion("openai.gpt-5.4") != "eu-central-1" || saved.SecretAccessKey != "SECRET" {
			t.Fatal("patch lost credentials, failed replacement, or failed default")
		}
		current, _ := manager.GetByID("mantle.json")
		public := mantlePublicMetadata(current)
		if _, ok := public["model_regions"]; !ok {
			t.Fatal("public overrides missing")
		}
		if bedrockmantle.FromMetadata(current.Metadata).ResolveRegion("openai.gpt-5.6-luna") != expected {
			t.Fatal("runtime map not updated")
		}
	}
}
