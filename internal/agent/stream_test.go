package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func writeModelChunk(w http.ResponseWriter, text, finish string) {
	data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": text}, "finish_reason": nil}}})
	if finish != "" {
		data, _ = json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}})
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func TestStreamTimeoutCancellationAndReloadInvalidateResponse(t *testing.T) {
	for _, name := range []string{"cancel", "timeout", "reload"} {
		t.Run(name, func(t *testing.T) {
			parser := waitingParser{make(chan struct{}, 1), make(chan struct{})}
			timeout := time.Second
			if name == "timeout" {
				timeout = 20 * time.Millisecond
			}
			s := NewServer(Config{Model: parser, ModelTimeout: timeout})
			cookie := session(t, s)
			call(t, s, cookie, "POST", "/api/import", testProject)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req := httptest.NewRequest("POST", "/api/message/stream", strings.NewReader(`{"mode":"model","text":"删除片段"}`)).WithContext(ctx)
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { s.ServeHTTP(rec, req); close(done) }()
			select {
			case <-parser.started:
			case <-time.After(time.Second):
				t.Fatal("parser did not start")
			}
			switch name {
			case "cancel":
				cancel()
			case "reload":
				call(t, s, cookie, "POST", "/api/import", testProject)
				close(parser.release)
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("stream did not stop")
			}
			if !strings.Contains(rec.Body.String(), "event: error") || strings.Contains(rec.Body.String(), "event: result") {
				t.Fatalf("stale or cancelled reply accepted: %s", rec.Body.String())
			}
			_, project := call(t, s, cookie, "GET", "/api/export", "")
			if project["revision"] != float64(1) {
				t.Fatal("failed stream changed revision")
			}
		})
	}
}

func TestStreamFormatRepairResetsDraftBeforeReturningPlan(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		text := `{"status":"answer","message":"未完成的咨询`
		if calls > 1 {
			text = `{"status":"plan","operations":[{"type":"delete","target_id":"a"}]}`
		}
		writeModelChunk(w, text, "")
		writeModelChunk(w, "", "stop")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()
	s := NewServer(Config{Model: HTTPModel{BaseURL: upstream.URL, Model: "test", Client: upstream.Client()}})
	cookie := session(t, s)
	call(t, s, cookie, "POST", "/api/import", testProject)
	req := httptest.NewRequest("POST", "/api/message/stream", strings.NewReader(`{"mode":"model","text":"删除 a"}`))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	text := rec.Body.String()
	if calls != 2 || !strings.Contains(text, "event: draft") || !strings.Contains(text, "event: reset") || strings.Index(text, "event: reset") > strings.Index(text, "event: result") {
		t.Fatalf("repair did not clear provisional reply: %s", text)
	}
	_, project := call(t, s, cookie, "GET", "/api/export", "")
	if project["revision"] != float64(1) {
		t.Fatal("repaired plan executed without confirmation")
	}
}

func TestConsultationDraftArrivesBeforeModelCompletes(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeModelChunk(w, `{"status":"answer","message":"你好`, "")
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		writeModelChunk(w, `，可以先查看预览。"}`, "")
		writeModelChunk(w, "", "stop")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()
	defer once.Do(func() { close(release) })
	s := NewServer(Config{Model: HTTPModel{BaseURL: upstream.URL, Model: "test", Client: upstream.Client()}})
	cookie := session(t, s)
	downstream := httptest.NewServer(s)
	defer downstream.Close()
	req, _ := http.NewRequest("POST", downstream.URL+"/api/message/stream", strings.NewReader(`{"mode":"model","text":"你好，如何预览？"}`))
	req.AddCookie(cookie)
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	draft := false
	var received []string
	for scanner.Scan() {
		line := scanner.Text()
		received = append(received, line)
		if strings.Contains(line, `"text":"你好"`) {
			draft = true
			break
		}
		if strings.Contains(line, "event: result") {
			t.Fatal("completed before release")
		}
	}
	if !draft {
		t.Fatalf("no draft before model completed: %v lines=%q", scanner.Err(), received)
	}
	once.Do(func() { close(release) })
	result := false
	for scanner.Scan() {
		if scanner.Text() == "event: result" {
			result = true
		}
	}
	if !result || scanner.Err() != nil {
		t.Fatalf("stream did not finish: %v", scanner.Err())
	}
}

func TestDraftDecoderNeverExposesPlansAndHandlesEscapedUnicode(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{"status":"plan","message":"删除 a","operations":`, ""},
		{`{"status":"answer","message":"你好\uD83D`, "你好"},
		{`{"status":"answer","message":"你好\uD83D\uDE00`, "你好😀"},
		{`{"status":"answer","message":"引号\"与换行\n`, "引号\"与换行\n"},
		{`{"message":"未声明状态`, ""},
	} {
		if got := answerDraft(tc.raw); got != tc.want {
			t.Errorf("draft=%q want=%q", got, tc.want)
		}
	}
}

func TestStreamRepliesAndFailuresKeepProjectUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name, content, finish string
		done                  bool
		wantEvent             string
	}{
		{"consultation", `{"status":"answer","message":"可以调整片段。"}`, "stop", true, "result"},
		{"plan", `{"status":"plan","operations":[{"type":"trim","target_id":"a","shorten_by_ms":1000}]}`, "stop", true, "result"},
		{"truncated JSON", `{"status":"answer","message":"未完成`, "stop", true, "error"},
		{"early disconnect", `{"status":"plan","operations":[{"type":"delete","target_id":"a"}]}`, "", false, "error"},
		{"missing done", `{"status":"answer","message":"看起来完整"}`, "stop", false, "error"},
		{"output limit", `{"status":"answer","message":"截断"}`, "length", true, "error"},
		{"illegal operation", `{"status":"plan","operations":[{"type":"execute","target_id":"a"}]}`, "stop", true, "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					Stream bool `json:"stream"`
				}
				_ = json.NewDecoder(r.Body).Decode(&payload)
				if !payload.Stream {
					t.Error("upstream request was not streamed")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				runes := []rune(tc.content)
				for _, part := range []string{string(runes[:len(runes)/2]), string(runes[len(runes)/2:])} {
					writeModelChunk(w, part, "")
				}
				if tc.finish != "" {
					writeModelChunk(w, "", tc.finish)
				}
				if tc.done {
					fmt.Fprint(w, "data: [DONE]\n\n")
				}
			}))
			defer upstream.Close()
			s := NewServer(Config{Model: HTTPModel{BaseURL: upstream.URL, Model: "test", Client: upstream.Client()}})
			cookie := session(t, s)
			call(t, s, cookie, "POST", "/api/import", testProject)
			_, before := call(t, s, cookie, "GET", "/api/export", "")
			req := httptest.NewRequest("POST", "/api/message/stream", bytes.NewBufferString(`{"mode":"model","text":"请帮我调整片段"}`))
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if !strings.Contains(rec.Header().Get("Content-Type"), "text/event-stream") || !strings.Contains(rec.Body.String(), "event: "+tc.wantEvent) {
				t.Fatalf("stream event missing: %s", rec.Body.String())
			}
			if tc.wantEvent == "error" && strings.Contains(rec.Body.String(), "event: result") {
				t.Fatal("failed stream reported success")
			}
			if tc.name == "plan" && strings.Contains(rec.Body.String(), "event: draft") {
				t.Fatal("unvalidated plan streamed as a draft")
			}
			_, after := call(t, s, cookie, "GET", "/api/export", "")
			if !reflect.DeepEqual(before, after) {
				t.Fatal("stream modified project without confirmation")
			}
		})
	}
}
