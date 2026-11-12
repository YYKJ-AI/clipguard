package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStage2SelectionAndClarificationThroughHTTP(t *testing.T) {
	s := NewServer(Config{})
	c := session(t, s)
	call(t, s, c, "POST", "/api/import", testProject)
	rec, without := call(t, s, c, "POST", "/api/message", `{"mode":"offline","text":"把这段缩短 1 秒"}`)
	if rec.Code != 200 || without["status"] != "clarify" || without["preview"] != nil {
		t.Fatalf("unselected request guessed: %s", rec.Body.String())
	}
	rec, with := call(t, s, c, "POST", "/api/message", `{"mode":"offline","text":"把这段缩短 1 秒","selected_clip_id":"a"}`)
	if rec.Code != 200 || with["status"] != "plan" {
		t.Fatalf("selected target was ignored: %s", rec.Body.String())
	}
	ops := with["preview"].(map[string]any)["operations"].([]any)
	if ops[0].(map[string]any)["target_id"] != "a" {
		t.Fatal("selection pointed to wrong target")
	}
	rec, _ = call(t, s, c, "POST", "/api/message", `{"mode":"offline","text":"把这段缩短 1 秒","selected_clip_id":"unknown"}`)
	if rec.Code != 400 {
		t.Fatal("unknown selected ID accepted")
	}
}

func TestStage2MissingAmountChangeMindAndCancel(t *testing.T) {
	s := NewServer(Config{})
	c := session(t, s)
	call(t, s, c, "POST", "/api/import", testProject)
	rec, first := call(t, s, c, "POST", "/api/message", `{"mode":"offline","text":"把 a 缩短"}`)
	if rec.Code != 200 || first["status"] != "clarify" {
		t.Fatalf("missing amount did not clarify: %s", rec.Body.String())
	}
	rec, completed := call(t, s, c, "POST", "/api/message", `{"mode":"offline","text":"1 秒"}`)
	if rec.Code != 200 || completed["status"] != "plan" {
		t.Fatalf("supplement did not complete instruction: %s", rec.Body.String())
	}
	plan := completed["preview"].(map[string]any)
	if plan["operations"].([]any)[0].(map[string]any)["target_id"] != "a" {
		t.Fatal("known target was lost")
	}
	rec, old := call(t, s, c, "POST", "/api/message", `{"mode":"offline","text":"把第二段缩短 1 秒"}`)
	if rec.Code != 200 || old["status"] != "clarify" {
		t.Fatalf("expected track clarification: %s", rec.Body.String())
	}
	rec, cancelled := call(t, s, c, "POST", "/api/message", `{"mode":"offline","text":"取消"}`)
	if rec.Code != 200 || cancelled["status"] != "answer" {
		t.Fatalf("cancel not handled: %s", rec.Body.String())
	}
	rec, _ = call(t, s, c, "POST", "/api/message", fmt.Sprintf(`{"mode":"offline","choice":"b","clarification_id":%q}`, old["clarification_id"]))
	if rec.Code != 409 {
		t.Fatal("old clarification survived cancellation")
	}
	rec, again := call(t, s, c, "POST", "/api/message", `{"mode":"offline","text":"把第二段缩短 1 秒"}`)
	if rec.Code != 200 || again["status"] != "clarify" {
		t.Fatal("track clarification missing")
	}
	rec, changed := call(t, s, c, "POST", "/api/message", `{"mode":"offline","text":"改成把 b 缩短 1 秒"}`)
	if rec.Code != 200 || changed["status"] != "plan" {
		t.Fatalf("change of mind not applied: %s", rec.Body.String())
	}
	if changed["preview"].(map[string]any)["operations"].([]any)[0].(map[string]any)["target_id"] != "b" {
		t.Fatal("old target leaked into replacement")
	}
}

func TestStage2RecentCaptionExpiresOnUndoAndReload(t *testing.T) {
	s := NewServer(Config{})
	c := session(t, s)
	call(t, s, c, "POST", "/api/import", testProject)
	rec, first := call(t, s, c, "POST", "/api/message", `{"mode":"offline","text":"把 s 的字幕改为“新”"}`)
	if rec.Code != 200 || first["status"] != "plan" {
		t.Fatal(rec.Body.String())
	}
	id := first["preview"].(map[string]any)["id"].(string)
	rec, _ = call(t, s, c, "POST", "/api/confirm", fmt.Sprintf(`{"plan_id":%q}`, id))
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	rec, recent := call(t, s, c, "POST", "/api/message", `{"mode":"offline","text":"把刚刚那个字幕改为“再见”"}`)
	if rec.Code != 200 || recent["status"] != "plan" {
		t.Fatalf("recent caption not resolved: %s", rec.Body.String())
	}
	if recent["preview"].(map[string]any)["operations"].([]any)[0].(map[string]any)["target_id"] != "s" {
		t.Fatal("wrong recent caption")
	}
	rec, _ = call(t, s, c, "POST", "/api/undo", `{"request_id":"stage2-undo"}`)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	rec, afterUndo := call(t, s, c, "POST", "/api/message", `{"mode":"offline","text":"把刚刚那个字幕改为“新”"}`)
	if rec.Code != 200 || afterUndo["status"] != "clarify" {
		t.Fatalf("undo left stale caption context: %s", rec.Body.String())
	}
	rec, _ = call(t, s, c, "POST", "/api/import", testProject)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	rec, afterReload := call(t, s, c, "POST", "/api/message", `{"mode":"offline","text":"把刚刚那个字幕改为“新”"}`)
	if rec.Code != 200 || afterReload["status"] != "clarify" {
		t.Fatalf("reload left stale caption context: %s", rec.Body.String())
	}
}

func TestStage2ModelSeesVerifiedSelectionOnly(t *testing.T) {
	var system string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []Turn `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		system = payload.Messages[0].Content
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": `{"status":"answer","message":"收到"}`}}}})
	}))
	defer upstream.Close()
	s := NewServer(Config{Model: HTTPModel{BaseURL: upstream.URL, Model: "test", Client: upstream.Client()}})
	c := session(t, s)
	call(t, s, c, "POST", "/api/import", testProject)
	rec, _ := call(t, s, c, "POST", "/api/message", `{"mode":"model","text":"这段怎么剪","selected_clip_id":"a"}`)
	if rec.Code != 200 || !strings.Contains(system, `"selected_clip_id":"a"`) {
		t.Fatalf("verified selection missing from model context: %s", rec.Body.String())
	}
	before := system
	rec, _ = call(t, s, c, "POST", "/api/message", `{"mode":"model","text":"这段怎么剪","selected_clip_id":"missing"}`)
	if rec.Code != 400 || system != before {
		t.Fatal("invalid selected ID reached provider")
	}
}

func TestStage2ProductEndpointKeepsRecentCaptionConsistent(t *testing.T) {
	s := NewServer(Config{ProductAuth: fixtureAuth{}})
	createProduct(t, s, "alice")
	id := productPlan(t, s, "alice", `把 s 的字幕改为“新”`)
	confirmed := productCall(t, s, "alice", "POST", "/api/v1/projects/p/plans/"+id+"/confirm", "{}", "")
	if confirmed.Code != 200 {
		t.Fatal(confirmed.Body.String())
	}
	response := productCall(t, s, "alice", "POST", "/api/v1/projects/p/messages", `{"mode":"offline","text":"把刚刚那个字幕改为“再见”"}`, "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"status":"plan"`) {
		t.Fatalf("product conversation lost confirmed caption: %s", response.Body.String())
	}
	undone := productCall(t, s, "alice", "POST", "/api/v1/projects/p/undo", `{"request_id":"stage2-product-undo"}`, "")
	if undone.Code != 200 {
		t.Fatal(undone.Body.String())
	}
	response = productCall(t, s, "alice", "POST", "/api/v1/projects/p/messages", `{"mode":"offline","text":"把刚刚那个字幕改为“再见”"}`, "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"status":"clarify"`) {
		t.Fatalf("product undo kept stale caption: %s", response.Body.String())
	}
}
