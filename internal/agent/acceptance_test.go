package agent

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// An invalid import must never silently turn an absent or null time into 0 ms.
func TestImportRejectsMissingOrNullClipFields(t *testing.T) {
	for _, tc := range []struct{ name, old, replacement string }{
		{"null start", `"start_ms":0`, `"start_ms":null`},
		{"missing start", `"start_ms":0,`, ``},
		{"null duration", `"duration_ms":5000`, `"duration_ms":null`},
		{"missing duration", `"duration_ms":5000,`, ``},
		{"null source in", `"source_in_ms":1000`, `"source_in_ms":null`},
		{"null video text", `"source_in_ms":1000`, `"text":null,"source_in_ms":1000`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := strings.Replace(testProject, tc.old, tc.replacement, 1)
			if raw == testProject {
				t.Fatal("test did not change its input")
			}
			if _, err := DecodeProject([]byte(raw)); err == nil {
				t.Error("invalid field accepted by project decoder")
			}
			s := NewServer(Config{})
			c := session(t, s)
			call(t, s, c, "POST", "/api/import", testProject)
			_, planned := call(t, s, c, "POST", "/api/message", `{"text":"把 a 缩短 2 秒"}`)
			planID := planned["preview"].(map[string]any)["id"]
			_, before := call(t, s, c, "GET", "/api/export", "")
			r, _ := call(t, s, c, "POST", "/api/import", raw)
			if r.Code != 400 {
				t.Errorf("invalid import returned %d", r.Code)
			}
			_, after := call(t, s, c, "GET", "/api/export", "")
			if !reflect.DeepEqual(before, after) {
				t.Fatal("invalid import replaced current project")
			}
			r, _ = call(t, s, c, "POST", "/api/confirm", fmt.Sprintf(`{"plan_id":%q}`, planID))
			if r.Code != 200 {
				t.Fatal("rejected import invalidated the current pending plan")
			}
			r, _ = call(t, s, c, "POST", "/api/undo", `{"request_id":"after-invalid-import"}`)
			if r.Code != 200 {
				t.Fatal("existing project could not be undone after invalid import")
			}
		})
	}
}

// Imported IDs and array order are data, not built-in addresses for the engine.
func TestImportedProjectIndependentOfIDsAndOrder(t *testing.T) {
	p := fixture(t)
	p.ID = "external-project"
	for i := range p.Tracks {
		p.Tracks[i].ID = "external-" + p.Tracks[i].ID
	}
	for i := range p.Clips {
		p.Clips[i].ID = "external-" + p.Clips[i].ID
		p.Clips[i].TrackID = "external-" + p.Clips[i].TrackID
	}
	for i, j := 0, len(p.Clips)-1; i < j; i, j = i+1, j-1 {
		p.Clips[i], p.Clips[j] = p.Clips[j], p.Clips[i]
	}
	for i, j := 0, len(p.Tracks)-1; i < j; i, j = i+1, j-1 {
		p.Tracks[i], p.Tracks[j] = p.Tracks[j], p.Tracks[i]
	}
	s := NewServer(Config{})
	c := session(t, s)
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := call(t, s, c, "POST", "/api/import", string(raw))
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	_, before := call(t, s, c, "GET", "/api/export", "")
	r, data := call(t, s, c, "POST", "/api/message", `{"text":"把 external-a 缩短 2 秒；把 external-s 的字幕改为“外部工程”"}`)
	if r.Code != 200 || data["status"] != "plan" {
		t.Fatalf("imported project not editable: %s", r.Body.String())
	}
	preview := data["preview"].(map[string]any)
	if preview["project_id"] != "external-project" || len(preview["changes"].([]any)) != 2 {
		t.Fatalf("preview bound to wrong project: %+v", preview)
	}
	_, current := call(t, s, c, "GET", "/api/export", "")
	if !reflect.DeepEqual(before, current) {
		t.Fatal("preview mutated external project")
	}
	r, data = call(t, s, c, "POST", "/api/confirm", fmt.Sprintf(`{"plan_id":%q}`, preview["id"]))
	if r.Code != 200 || len(data["changes"].([]any)) != 2 {
		t.Fatalf("commit failed: %s", r.Body.String())
	}
	for _, rawClip := range data["project"].(map[string]any)["clips"].([]any) {
		clip := rawClip.(map[string]any)
		if clip["id"] == "external-a" && clip["duration_ms"] != float64(3000) {
			t.Fatal("wrong video trimmed")
		}
		if clip["id"] == "external-s" && clip["text"] != "外部工程" {
			t.Fatal("wrong subtitle edited")
		}
	}
	r, data = call(t, s, c, "POST", "/api/undo", `{"request_id":"external-undo"}`)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	restored := data["project"].(map[string]any)
	if restored["revision"] != float64(3) {
		t.Fatal("undo did not advance version")
	}
	restored["revision"] = float64(1)
	if !reflect.DeepEqual(before, restored) {
		t.Fatal("undo did not restore imported content and order")
	}
}
