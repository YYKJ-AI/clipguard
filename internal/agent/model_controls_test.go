package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestHTTPModelRequestControls(t *testing.T) {
	for _, mode := range []string{"disabled", "enabled", ""} {
		t.Run(mode, func(t *testing.T) {
			var observed map[string]any
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&observed); err != nil {
					t.Error(err)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": `{"status":"clarify","message":"请说明缩短多少秒"}`}}}})
			}))
			defer upstream.Close()
			m := HTTPModel{BaseURL: upstream.URL, Model: "test", Client: upstream.Client(), JSONMode: true, ThinkingMode: mode, MaxTokens: 4096}
			result, err := m.Parse(context.Background(), fixture(t), []Turn{{Role: "user", Content: "把开头缩短一点"}})
			if err != nil || result.Status != "clarify" {
				t.Fatalf("model request failed: %v", err)
			}
			if observed["max_tokens"] != float64(4096) {
				t.Errorf("output budget not transmitted: %v", observed["max_tokens"])
			}
			if mode == "" {
				if _, ok := observed["thinking"]; ok {
					t.Fatal("unspecified thinking setting must be omitted")
				}
			} else {
				thinking, ok := observed["thinking"].(map[string]any)
				if !ok || thinking["type"] != mode {
					t.Fatalf("thinking setting not transmitted: %v", observed["thinking"])
				}
			}
		})
	}
}

func TestHTTPModelInvalidControlsDoNotCallProvider(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(400)
	}))
	defer upstream.Close()
	for _, settings := range []struct {
		mode   string
		tokens int
	}{{"invalid", 4096}, {"disabled", -1}} {
		m := HTTPModel{BaseURL: upstream.URL, Model: "test", Client: upstream.Client(), ThinkingMode: settings.mode, MaxTokens: settings.tokens}
		if _, err := m.Parse(context.Background(), fixture(t), nil); err == nil {
			t.Fatal("invalid control configuration accepted")
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("invalid settings sent %d billable requests", requests.Load())
	}
}
