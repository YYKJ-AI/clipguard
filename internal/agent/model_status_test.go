package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtensionModelCheckVerifiesJSONAndStreamWithoutEditing(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload struct {
			Stream bool `json:"stream"`
		}
		json.NewDecoder(r.Body).Decode(&payload)
		content := `{"status":"answer","message":"可以调整片段。"}`
		if payload.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			writeModelChunk(w, content, "")
			writeModelChunk(w, "", "stop")
			fmt.Fprint(w, "data: [DONE]\n\n")
		} else {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}, "finish_reason": "stop"}}})
		}
	}))
	defer upstream.Close()
	s := NewServer(Config{Model: HTTPModel{BaseURL: upstream.URL, Model: "local-test", Client: upstream.Client()}})
	c := session(t, s)
	call(t, s, c, "POST", "/api/import", testProject)
	r, status := call(t, s, c, "GET", "/api/model/status", "")
	if r.Code != 200 || status["state"] != "pending" {
		t.Fatalf("configured model incorrectly marked adapted: %s", r.Body.String())
	}
	r, status = call(t, s, c, "POST", "/api/model/check", `{}`)
	if r.Code != 200 || status["state"] != "connected" || calls != 2 {
		t.Fatalf("both protocols not verified: %s calls=%d", r.Body.String(), calls)
	}
	_, p := call(t, s, c, "GET", "/api/export", "")
	if p["revision"] != float64(1) {
		t.Fatal("connection check edited project")
	}
}

func TestExtensionModelCheckMissingAndFailure(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(fmt.Sprint(configured), func(t *testing.T) {
			config := Config{}
			var upstream *httptest.Server
			if configured {
				upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429) }))
				defer upstream.Close()
				config.Model = HTTPModel{BaseURL: upstream.URL, Model: "test", Client: upstream.Client()}
			}
			s := NewServer(config)
			c := session(t, s)
			r, data := call(t, s, c, "POST", "/api/model/check", `{}`)
			if configured {
				if r.Code != 200 || data["state"] != "unavailable" {
					t.Fatalf("upstream failure reported connected: %s", r.Body.String())
				}
			} else if r.Code != 503 {
				t.Fatal("missing model should be explicit")
			}
		})
	}
}
