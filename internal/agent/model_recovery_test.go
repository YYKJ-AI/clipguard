package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestMalformedModelResponseRecovery(t *testing.T) {
	for _, tc := range []struct {
		name, second string
		wantStatus   int
		wantCalls    int
	}{
		{"corrected clarification", `{"status":"clarify","message":"你希望修改哪个片段？"}`, 200, 2},
		{"corrected plan still requires confirmation", `{"status":"plan","operations":[{"type":"trim","target_id":"a","shorten_by_ms":1000}]}`, 200, 2},
		{"repeated array rejected", `[{"type":"delete","target_id":"b"}]`, 422, 2},
		{"extra operation field rejected", `{"status":"plan","operations":[{"type":"delete","target_id":"b","execute":true}]}`, 422, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var payload struct {
					Messages []Turn `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if len(payload.Messages) < 2 || payload.Messages[len(payload.Messages)-1].Content != "请帮我调整片段" {
					t.Error("original input lost during recovery")
				}
				content := `[{"type":"delete","target_id":"b"}]`
				if calls > 1 {
					content = tc.second
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}}})
			}))
			defer upstream.Close()
			s := NewServer(Config{Model: HTTPModel{BaseURL: upstream.URL, Model: "test", Client: upstream.Client()}})
			cookie := session(t, s)
			call(t, s, cookie, "POST", "/api/import", testProject)
			_, before := call(t, s, cookie, "GET", "/api/export", "")
			rec, _ := call(t, s, cookie, "POST", "/api/message", `{"mode":"model","text":"请帮我调整片段"}`)
			if rec.Code != tc.wantStatus || calls != tc.wantCalls {
				t.Fatalf("status=%d calls=%d body=%s", rec.Code, calls, rec.Body.String())
			}
			if rec.Code == 422 && (strings.Contains(rec.Body.String(), "cannot unmarshal") || strings.Contains(rec.Body.String(), "agent.ParseResult")) {
				t.Fatal("internal parser details shown to user")
			}
			if rec.Code == 422 && strings.Contains(rec.Body.String(), "离线模式使用阿拉伯数字") {
				t.Fatal("model failure incorrectly instructs user to follow offline syntax")
			}
			_, after := call(t, s, cookie, "GET", "/api/export", "")
			if !reflect.DeepEqual(before, after) {
				t.Fatal("response recovery modified project without confirmation")
			}
		})
	}
}

func TestProviderFailureNotRetried(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(429) }))
	defer upstream.Close()
	s := NewServer(Config{Model: HTTPModel{BaseURL: upstream.URL, Model: "test", Client: upstream.Client()}})
	cookie := session(t, s)
	call(t, s, cookie, "POST", "/api/import", testProject)
	rec, _ := call(t, s, cookie, "POST", "/api/message", `{"mode":"model","text":"删除 b"}`)
	if rec.Code != 422 || calls != 1 {
		t.Fatalf("provider failure retried: status=%d calls=%d", rec.Code, calls)
	}
}
