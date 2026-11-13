package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

// These HTTP tests catch redo consuming the wrong batch, stale plan reuse,
// cancelled edits entering history, and history leaking across sessions/reloads.
func TestExtensionHistoryAndRedoLifecycle(t *testing.T) {
	s := NewServer(Config{})
	c := session(t, s)
	call(t, s, c, "POST", "/api/import", testProject)
	_, data := call(t, s, c, "POST", "/api/message", `{"text":"把 a 缩短 1 秒；把 s 的字幕改为“新字幕”"}`)
	plan := data["preview"].(map[string]any)["id"].(string)
	r, h := call(t, s, c, "GET", "/api/history", "")
	if r.Code != 200 {
		t.Fatalf("history unavailable: %s", r.Body.String())
	}
	if len(h["entries"].([]any)) != 0 {
		t.Fatal("preview entered committed history")
	}
	call(t, s, c, "POST", "/api/confirm", fmt.Sprintf(`{"plan_id":%q}`, plan))
	_, h = call(t, s, c, "GET", "/api/history", "")
	if !h["can_undo"].(bool) || h["can_redo"].(bool) || len(h["entries"].([]any)) != 1 {
		t.Fatal("commit history incorrect")
	}
	// A queued plan must become stale even when undo/redo restores identical content.
	_, data = call(t, s, c, "POST", "/api/message", `{"text":"删除 d"}`)
	stale := data["preview"].(map[string]any)["id"].(string)
	call(t, s, c, "POST", "/api/undo", `{"request_id":"u1"}`)
	r, data = call(t, s, c, "POST", "/api/redo", `{"request_id":"r1"}`)
	if r.Code != 200 {
		t.Fatalf("redo unavailable: %s", r.Body.String())
	}
	p := data["project"].(map[string]any)
	if p["revision"] != float64(4) {
		t.Fatal("redo revision must advance")
	}
	for _, raw := range p["clips"].([]any) {
		clip := raw.(map[string]any)
		if clip["id"] == "a" && clip["duration_ms"] != float64(4000) {
			t.Fatal("redo did not restore trim")
		}
		if clip["id"] == "s" && clip["text"] != "新字幕" {
			t.Fatal("redo lost batch caption")
		}
	}
	if r, _ = call(t, s, c, "POST", "/api/confirm", fmt.Sprintf(`{"plan_id":%q}`, stale)); r.Code != 409 {
		t.Fatal("stale plan applied")
	}
	if r, _ = call(t, s, c, "POST", "/api/redo", `{"request_id":"r1"}`); r.Code != 409 {
		t.Fatal("duplicate redo accepted")
	}
	_, h = call(t, s, c, "GET", "/api/history", "")
	if len(h["entries"].([]any)) != 3 {
		t.Fatal("expected commit, undo, redo records")
	}
	call(t, s, c, "POST", "/api/undo", `{"request_id":"u2"}`)
	_, data = call(t, s, c, "POST", "/api/message", `{"text":"删除 d"}`)
	call(t, s, c, "POST", "/api/confirm", fmt.Sprintf(`{"plan_id":%q}`, data["preview"].(map[string]any)["id"]))
	if r, _ = call(t, s, c, "POST", "/api/redo", `{"request_id":"r2"}`); r.Code != 409 {
		t.Fatal("new edit did not clear redo branch")
	}
	other := session(t, s)
	_, h = call(t, s, other, "GET", "/api/history", "")
	if len(h["entries"].([]any)) != 0 {
		t.Fatal("foreign history exposed")
	}
	call(t, s, c, "POST", "/api/import", testProject)
	_, h = call(t, s, c, "GET", "/api/history", "")
	if len(h["entries"].([]any)) != 0 || h["can_redo"].(bool) || h["can_undo"].(bool) {
		t.Fatal("reload retained history")
	}
}

func TestExtensionInsertValidationAndAtomicity(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		valid     bool
	}{
		{"video", `[{"type":"insert","target_id":"new","source_id":"a","track_id":"v2","start_ms":12000}]`, true},
		{"caption", `[{"type":"insert","target_id":"new","source_id":"s","track_id":"c1","start_ms":12000}]`, true},
		{"duplicate", `[{"type":"insert","target_id":"a","source_id":"a","track_id":"v2","start_ms":12000}]`, false},
		{"collision", `[{"type":"insert","target_id":"new","source_id":"a","track_id":"v1","start_ms":0}]`, false},
		{"locked", `[{"type":"insert","target_id":"new","source_id":"s","track_id":"locked","start_ms":12000}]`, false},
		{"type mismatch", `[{"type":"insert","target_id":"new","source_id":"a","track_id":"c1","start_ms":12000}]`, false},
		{"unknown source", `[{"type":"insert","target_id":"new","source_id":"missing","track_id":"v2","start_ms":12000}]`, false},
		{"negative", `[{"type":"insert","target_id":"new","source_id":"a","track_id":"v2","start_ms":-1}]`, false},
		{"extra field", `[{"type":"insert","target_id":"new","source_id":"a","track_id":"v2","start_ms":12000,"text":"injected"}]`, false},
		{"source field on delete", `[{"type":"delete","target_id":"a","source_id":"s"}]`, false},
		{"failed second", `[{"type":"insert","target_id":"new","source_id":"a","track_id":"v2","start_ms":12000},{"type":"delete","target_id":"l"}]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := fixture(t)
			before := Clone(p)
			var ops []Operation
			err := json.Unmarshal([]byte(tc.raw), &ops)
			var next Project
			var diff []Change
			if err == nil {
				next, diff, err = Apply(p, ops)
			}
			if tc.valid {
				if err != nil {
					t.Fatal(err)
				}
				if len(next.Clips) != 7 || len(diff) != 1 || diff[0].Before != nil || diff[0].After.ID != "new" {
					t.Fatal("insertion result wrong")
				}
			} else if err == nil {
				t.Fatal("invalid insert accepted")
			}
			if !reflect.DeepEqual(p, before) {
				t.Fatal("input mutated")
			}
		})
	}
}

func TestExtensionOfflineInsertAndUndo(t *testing.T) {
	p := fixture(t)
	result, err := (OfflineParser{}).Parse(context.Background(), p, []Turn{{Role: "user", Content: "在 v2 轨道的 12 秒处插入 a；把 s 的字幕改为“副本”"}})
	if err != nil || result.Status != "plan" || len(result.Operations) != 2 {
		t.Fatalf("offline insert missing: %+v %v", result, err)
	}
	next, diff, err := Apply(p, result.Operations)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Clips) != 7 || len(diff) != 2 || result.Operations[0].TargetID == "a" {
		t.Fatal("source reused instead of new ID")
	}
}

func TestExtensionInsertSelectedCaption(t *testing.T) {
	result, err := (OfflineParser{selectedID: "s"}).Parse(context.Background(), fixture(t), []Turn{{Role: "user", Content: "在 c1 轨道的 12 秒处插入这段"}})
	if err != nil || result.Status != "plan" || len(result.Operations) != 1 || result.Operations[0].SourceID == nil || *result.Operations[0].SourceID != "s" {
		t.Fatalf("selected caption was not inserted: %+v %v", result, err)
	}
}

func TestExtensionInsertPreviewConfirmUndoRedo(t *testing.T) {
	s := NewServer(Config{})
	c := session(t, s)
	call(t, s, c, "POST", "/api/import", testProject)
	r, data := call(t, s, c, "POST", "/api/insert-preview", `{"source_id":"a","track_id":"v2","start_ms":12000}`)
	if r.Code != 200 || data["status"] != "plan" {
		t.Fatalf("insert preview unavailable: %s", r.Body.String())
	}
	_, original := call(t, s, c, "GET", "/api/export", "")
	if len(original["clips"].([]any)) != 6 {
		t.Fatal("preview applied")
	}
	id := data["preview"].(map[string]any)["id"].(string)
	call(t, s, c, "POST", "/api/confirm", fmt.Sprintf(`{"plan_id":%q}`, id))
	call(t, s, c, "POST", "/api/undo", `{"request_id":"insert-u"}`)
	r, data = call(t, s, c, "POST", "/api/redo", `{"request_id":"insert-r"}`)
	if r.Code != 200 || len(data["project"].(map[string]any)["clips"].([]any)) != 7 {
		t.Fatal("insert redo lost content")
	}
	// Cancelling a subsequent insertion does not add a history entry.
	_, data = call(t, s, c, "POST", "/api/insert-preview", `{"source_id":"s","track_id":"c1","start_ms":12000}`)
	call(t, s, c, "POST", "/api/cancel", fmt.Sprintf(`{"plan_id":%q}`, data["preview"].(map[string]any)["id"]))
	_, h := call(t, s, c, "GET", "/api/history", "")
	if len(h["entries"].([]any)) != 3 {
		t.Fatal("cancel entered history")
	}
}

func TestExtensionHistoryDetachedAndRedoOrder(t *testing.T) {
	w := loaded(t)
	p := previewTrim(t, w, 4000)
	w.Confirm(p.ID)
	p = previewTrim(t, w, 3000)
	w.Confirm(p.ID)
	w.Undo("one")
	w.Undo("two")
	one, err := w.Redo("r-one")
	if err != nil || one.Project.Clips[0].DurationMS != 4000 {
		t.Fatal("redo order incorrect")
	}
	if _, err = w.Redo("r-one"); err == nil {
		t.Fatal("duplicate redo consumed next entry")
	}
	two, err := w.Redo("r-two")
	if err != nil || two.Project.Clips[0].DurationMS != 3000 {
		t.Fatal("redo lost next entry")
	}
	h := w.History()
	h.Entries[0].Changes[0].After.DurationMS = 1
	if w.History().Entries[0].Changes[0].After.DurationMS != 4000 {
		t.Fatal("history leaked mutable pointers")
	}
}
