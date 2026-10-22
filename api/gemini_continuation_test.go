package api

import (
	"bytes"
	"cornerstone/config"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProviderGeminiAutomaticContinuationAPI(t *testing.T) {
	for _, memory := range []bool{false, true} {
		t.Run(map[bool]string{false: "chat", true: "memory"}[memory], func(t *testing.T) {
			p := config.DefaultProvider()
			p.ID = "provider-1"
			p.Type = config.ProviderTypeGemini
			p.Model = "fixture-model"
			p.APIKey = "fixture-key"
			h := &Handler{configManager: newTestProviderConfigManager(t, p)}
			if memory {
				h.configManager = newTestConfigManagerWithMemoryProvider(t, p)
			}
			for _, tc := range []struct {
				kind  string
				value any
				want  bool
			}{
				{"gemini", nil, false}, {"gemini", true, true}, {"gemini", nil, true}, {"gemini", false, false}, {"gemini", true, true}, {"openai", true, false}, {"gemini", nil, false},
			} {
				payload := map[string]any{"type": tc.kind, "model": "fixture-model", "name": "fixture"}
				if tc.value != nil {
					payload["gemini_automatic_continuation"] = tc.value
				}
				path := "/management/providers/provider-1"
				var body any = payload
				if memory {
					path = "/management/memory-provider"
					body = map[string]any{"use_custom": true, "provider": payload}
				}
				data, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(data))
				if memory {
					h.handleMemoryProvider(rec, req)
				} else {
					h.handleProviderByID(rec, req)
				}
				if rec.Code != http.StatusOK {
					t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
				}
				saved := h.configManager.GetProvider(p.ID)
				if memory {
					saved = h.configManager.Get().MemoryProvider
				}
				if saved == nil || saved.GeminiAutomaticContinuation != tc.want {
					t.Fatalf("%+v: got %+v", tc, saved)
				}
				get := httptest.NewRecorder()
				getReq := httptest.NewRequest(http.MethodGet, path, nil)
				if memory {
					h.handleMemoryProvider(get, getReq)
				} else {
					h.handleProviderByID(get, getReq)
				}
				if get.Code != http.StatusOK {
					t.Fatalf("GET status %d: %s", get.Code, get.Body.String())
				}
				var response struct {
					Data json.RawMessage `json:"data"`
				}
				if err := json.Unmarshal(get.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(response.Data, &fields); err != nil {
					t.Fatal(err)
				}
				if memory {
					if err := json.Unmarshal(fields["memory_provider"], &fields); err != nil {
						t.Fatal(err)
					}
				}
				var flag bool
				if fields["gemini_automatic_continuation"] == nil {
					t.Fatalf("GET omitted flag: %s", get.Body.String())
				}
				if err := json.Unmarshal(fields["gemini_automatic_continuation"], &flag); err != nil {
					t.Fatal(err)
				}
				if flag != tc.want {
					t.Fatalf("GET flag=%v, want %v", flag, tc.want)
				}
				// The shared builder is used by Web, external channels, and tool-loop requests.
				if got := buildChatRequestForProvider(saved, nil, nil, false, 0, nil); got.GeminiAutomaticContinuation != tc.want {
					t.Fatalf("request flag = %v, want %v", got.GeminiAutomaticContinuation, tc.want)
				}
			}
		})
	}
}

func TestCreateProviderGeminiAutomaticContinuation(t *testing.T) {
	for _, kind := range []string{"gemini", "openai"} {
		for _, value := range []any{nil, false, true} {
			p := config.DefaultProvider()
			p.ID = "existing"
			h := &Handler{configManager: newTestProviderConfigManager(t, p)}
			body := map[string]any{"id": "created", "type": kind, "name": "fixture", "model": "fixture-model", "api_key": "fixture-key"}
			if value != nil {
				body["gemini_automatic_continuation"] = value
			}
			data, _ := json.Marshal(body)
			rec := httptest.NewRecorder()
			h.handleProviders(rec, httptest.NewRequest(http.MethodPost, "/management/providers", bytes.NewReader(data)))
			if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			providers := h.configManager.GetProviders()
			created := providers[len(providers)-1]
			if created.GeminiAutomaticContinuation != (kind == "gemini" && value == true) {
				t.Fatalf("unexpected created provider: %+v", created)
			}
		}
	}
}

func TestBuildChatRequestIgnoresNonGeminiContinuation(t *testing.T) {
	for _, kind := range []config.ProviderType{config.ProviderTypeOpenAI, config.ProviderTypeOpenAIResponse, config.ProviderTypeAnthropic} {
		p := config.DefaultProvider()
		p.Type = kind
		p.GeminiAutomaticContinuation = true
		if req := buildChatRequestForProvider(&p, nil, nil, false, 0, nil); req.GeminiAutomaticContinuation {
			t.Fatalf("flag leaked to %s", kind)
		}
	}
}
