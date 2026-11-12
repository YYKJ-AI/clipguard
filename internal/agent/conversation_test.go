package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestModelReceivesTrackOrderIndependentOfClipArray(t *testing.T) {
	p := fixture(t)
	for i, j := 0, len(p.Clips)-1; i < j; i, j = i+1, j-1 {
		p.Clips[i], p.Clips[j] = p.Clips[j], p.Clips[i]
	}
	before := Clone(p)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []Turn `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if !strings.Contains(payload.Messages[0].Content, `"v1":["a","b"]`) || !strings.Contains(payload.Messages[0].Content, `"v2":["c","d"]`) {
			t.Error("model lacks per-track chronological order")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": `{"status":"clarify","message":"请选择轨道"}`}}}})
	}))
	defer upstream.Close()
	_, err := (HTTPModel{BaseURL: upstream.URL, Model: "test", Client: upstream.Client()}).Parse(context.Background(), p, []Turn{{Role: "user", Content: "把第二段移到开头"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p, before) {
		t.Fatal("model context construction mutated project")
	}
}

func TestAnswerResponseHasNoEdits(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		valid         bool
	}{
		{"editing explanation", `{"status":"answer","message":"裁剪是改变片段长度。"}`, true},
		{"answer with edit", `{"status":"answer","message":"已回答","operations":[{"type":"delete","target_id":"b"}]}`, false},
		{"empty answer", `{"status":"answer","message":" "}`, false},
		{"answer with candidates", `{"status":"answer","message":"回答","candidates":[{"id":"b","label":"b"}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": tc.content}}}})
			}))
			defer upstream.Close()
			s := NewServer(Config{Model: HTTPModel{BaseURL: upstream.URL, Model: "test", Client: upstream.Client()}})
			c := session(t, s)
			call(t, s, c, "POST", "/api/import", testProject)
			_, before := call(t, s, c, "GET", "/api/export", "")
			rec, data := call(t, s, c, "POST", "/api/message", `{"mode":"model","text":"什么是裁剪？"}`)
			if tc.valid {
				if rec.Code != 200 || data["status"] != "answer" || data["message"] != "裁剪是改变片段长度。" {
					t.Fatal(rec.Body.String())
				}
				if _, ok := data["preview"]; ok {
					t.Fatal("answer has edit preview")
				}
			} else if rec.Code == 200 {
				t.Fatal("unsafe answer accepted")
			}
			_, after := call(t, s, c, "GET", "/api/export", "")
			if !reflect.DeepEqual(before, after) {
				t.Fatal("chat modified project")
			}
		})
	}
}

func TestConversationBeforeImportAndAcrossManyTurns(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload struct {
			Messages []Turn `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if calls > 1 {
			joined := ""
			for _, turn := range payload.Messages {
				joined += turn.Content
			}
			if !strings.Contains(joined, fmt.Sprintf("回答-%d", calls-1)) {
				t.Error("last conversation answer missing")
			}
		}
		content := fmt.Sprintf(`{"status":"answer","message":"回答-%d"}`, calls)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}}})
	}))
	defer upstream.Close()
	s := NewServer(Config{Model: HTTPModel{BaseURL: upstream.URL, Model: "test", Client: upstream.Client()}})
	c := session(t, s)
	for n := 1; n <= 8; n++ {
		rec, data := call(t, s, c, "POST", "/api/message", fmt.Sprintf(`{"mode":"model","text":"问题-%d"}`, n))
		if rec.Code != 200 || data["status"] != "answer" {
			t.Fatalf("turn %d: %s", n, rec.Body.String())
		}
	}
	_, state := call(t, s, c, "GET", "/api/state", "")
	if state["project"] != nil {
		t.Fatal("chat created fictional project")
	}
}

func TestModelPlanBeforeImportCannotCreateProject(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": `{"status":"plan","operations":[{"type":"delete","target_id":"b"}]}`}}}})
	}))
	defer upstream.Close()
	s := NewServer(Config{Model: HTTPModel{BaseURL: upstream.URL, Model: "test", Client: upstream.Client()}})
	c := session(t, s)
	rec, _ := call(t, s, c, "POST", "/api/message", `{"mode":"model","text":"删除 b"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"clarify"`) || strings.Contains(rec.Body.String(), `"preview"`) {
		t.Fatal("plan without project must become load clarification")
	}
	_, state := call(t, s, c, "GET", "/api/state", "")
	if state["project"] != nil {
		t.Fatal("unexpected project")
	}
}

func TestChatDoesNotConsumeExistingPreviewOrUndo(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": `{"status":"answer","message":"裁剪是改变片段长度。"}`}}}})
	}))
	defer upstream.Close()
	s := NewServer(Config{Model: HTTPModel{BaseURL: upstream.URL, Model: "test", Client: upstream.Client()}})
	c := session(t, s)
	call(t, s, c, "POST", "/api/import", testProject)
	rec, plan := call(t, s, c, "POST", "/api/message", `{"mode":"offline","text":"把 a 缩短 1 秒"}`)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	id := plan["preview"].(map[string]any)["id"].(string)
	rec, _ = call(t, s, c, "POST", "/api/message", `{"mode":"model","text":"什么是裁剪？"}`)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	rec, _ = call(t, s, c, "POST", "/api/confirm", fmt.Sprintf(`{"plan_id":%q}`, id))
	if rec.Code != 200 {
		t.Fatal("chat invalidated existing preview")
	}
	rec, result := call(t, s, c, "POST", "/api/undo", `{"request_id":"chat-undo"}`)
	if rec.Code != 200 || result["project"].(map[string]any)["revision"] != float64(3) {
		t.Fatal("chat consumed undo history")
	}
}
