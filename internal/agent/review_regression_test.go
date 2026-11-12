package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// Selecting a target must never replace matching words inside caption text.
func TestClarificationPreservesQuotedText(t *testing.T) {
	for _, caption := range []string{"第二段", "第二段；开场片段", `带有“第二段”的正文`} {
		t.Run(caption, func(t *testing.T) {
			s := NewServer(Config{})
			c := session(t, s)
			call(t, s, c, "POST", "/api/import", testProject)
			_, before := call(t, s, c, "GET", "/api/export", "")
			text := fmt.Sprintf("把 s 的字幕改为%q；删除第二段", caption)
			request, _ := json.Marshal(map[string]string{"text": text})
			r, data := call(t, s, c, "POST", "/api/message", string(request))
			if r.Code != 200 || data["status"] != "clarify" {
				t.Fatalf("expected clarification: %s", r.Body.String())
			}
			r, data = call(t, s, c, "POST", "/api/message", fmt.Sprintf(`{"choice":"b","clarification_id":%q}`, data["clarification_id"]))
			if r.Code != 200 || data["status"] != "plan" {
				t.Fatalf("one selection should resolve the target: %s", r.Body.String())
			}
			var preview Preview
			raw, _ := json.Marshal(data["preview"])
			if err := json.Unmarshal(raw, &preview); err != nil {
				t.Fatal(err)
			}
			if len(preview.Operations) != 2 || preview.Operations[0].Text == nil || *preview.Operations[0].Text != caption || preview.Operations[1].TargetID != "b" {
				t.Fatalf("selection changed the requested operations: %+v", preview.Operations)
			}
			_, current := call(t, s, c, "GET", "/api/export", "")
			if !reflect.DeepEqual(before, current) {
				t.Fatal("clarification or preview changed the project")
			}
			r, _ = call(t, s, c, "POST", "/api/confirm", fmt.Sprintf(`{"plan_id":%q}`, preview.ID))
			if r.Code != 200 {
				t.Fatal(r.Body.String())
			}
			_, current = call(t, s, c, "GET", "/api/export", "")
			for _, value := range current["clips"].([]any) {
				clip := value.(map[string]any)
				if clip["id"] == "b" {
					t.Fatal("selected clip was not deleted")
				}
				if clip["id"] == "s" && clip["text"] != caption {
					t.Fatalf("wrong committed caption: %v", clip["text"])
				}
			}
		})
	}
}

// Resolving a second ambiguity must retain the user's first choice.
func TestMultipleClarificationsRetainChoices(t *testing.T) {
	s := NewServer(Config{})
	c := session(t, s)
	call(t, s, c, "POST", "/api/import", testProject)
	r, data := call(t, s, c, "POST", "/api/message", `{"text":"把第二段移到 10 秒处；把开场片段缩短 1 秒"}`)
	for _, choice := range []string{"b", "c"} {
		if r.Code != 200 || data["status"] != "clarify" {
			t.Fatalf("missing clarification for %s: %s", choice, r.Body.String())
		}
		r, data = call(t, s, c, "POST", "/api/message", fmt.Sprintf(`{"choice":%q,"clarification_id":%q}`, choice, data["clarification_id"]))
	}
	if r.Code != 200 || data["status"] != "plan" {
		t.Fatalf("choices did not produce a complete plan: %s", r.Body.String())
	}
	ops := data["preview"].(map[string]any)["operations"].([]any)
	if ops[0].(map[string]any)["target_id"] != "b" || ops[1].(map[string]any)["target_id"] != "c" || ops[1].(map[string]any)["duration_ms"] != float64(3000) {
		t.Fatalf("lost selected targets: %+v", ops)
	}
}

func TestInvalidLockImportPreservesCurrentProject(t *testing.T) {
	for _, field := range []string{`"locked":null`, `"locked":"false"`, `"locked":0`, ""} {
		t.Run(field, func(t *testing.T) {
			old := `"id":"locked","kind":"caption","locked":true`
			replacement := `"id":"locked","kind":"caption"`
			if field != "" {
				replacement += "," + field
			}
			raw := strings.Replace(testProject, old, replacement, 1)
			if _, err := DecodeProject([]byte(raw)); err == nil {
				t.Error("invalid lock field accepted by project decoder")
			}
			s := NewServer(Config{})
			c := session(t, s)
			call(t, s, c, "POST", "/api/import", testProject)
			_, before := call(t, s, c, "GET", "/api/export", "")
			r, _ := call(t, s, c, "POST", "/api/import", raw)
			if r.Code != http.StatusBadRequest {
				t.Errorf("invalid import returned %d", r.Code)
			}
			_, after := call(t, s, c, "GET", "/api/export", "")
			if !reflect.DeepEqual(before, after) {
				t.Fatal("rejected import replaced the current project")
			}
		})
	}
}

func TestUnchangedPrefixAllowsLaterEdit(t *testing.T) {
	for _, text := range []string{
		"把 a 移到开头；把 a 缩短 2 秒",
		"把 b 移到 10 秒处；把 b 移到 6 秒处；把 a 缩短 2 秒",
	} {
		t.Run(text, func(t *testing.T) {
			s := NewServer(Config{})
			c := session(t, s)
			call(t, s, c, "POST", "/api/import", testProject)
			_, before := call(t, s, c, "GET", "/api/export", "")
			raw, _ := json.Marshal(map[string]string{"text": text})
			r, data := call(t, s, c, "POST", "/api/message", string(raw))
			if r.Code != 200 || data["status"] != "plan" {
				t.Fatalf("valid batch refused: %s", r.Body.String())
			}
			preview := data["preview"].(map[string]any)
			_, current := call(t, s, c, "GET", "/api/export", "")
			if !reflect.DeepEqual(before, current) {
				t.Fatal("preview mutated project")
			}
			r, data = call(t, s, c, "POST", "/api/confirm", fmt.Sprintf(`{"plan_id":%q}`, preview["id"]))
			if r.Code != 200 {
				t.Fatal(r.Body.String())
			}
			p := data["project"].(map[string]any)
			if p["revision"] != float64(2) || len(data["changes"].([]any)) != 1 {
				t.Fatalf("wrong commit: %+v", data)
			}
			for _, value := range p["clips"].([]any) {
				clip := value.(map[string]any)
				if clip["id"] == "a" && clip["duration_ms"] != float64(3000) {
					t.Fatal("later edit was not applied")
				}
			}
			r, _ = call(t, s, c, "POST", "/api/undo", `{"request_id":"whole-batch"}`)
			if r.Code != 200 {
				t.Fatal(r.Body.String())
			}
			_, current = call(t, s, c, "GET", "/api/export", "")
			current["revision"] = float64(1)
			if !reflect.DeepEqual(before, current) {
				t.Fatal("undo did not restore the complete batch")
			}
		})
	}
}

// An unsupported clause must not cause a supported prefix to be applied.
func TestInstructionFallbackPreservesState(t *testing.T) {
	for _, text := range []string{
		"把 a 缩短两秒", "剪得紧凑一点", "不要删除 a",
		"把 a 缩短 2 秒；把第二段挪到第一段后面", "把 a 缩短 2 秒；删除 unknown",
		"把 a 缩短 2 秒；把 l 的字幕改为“新文字”", "把 s 的字幕改为没有引号",
		"把 a 移到开头；把 a 移到开头", "把 a 缩短 2 秒；把 a 缩短 20 秒",
	} {
		t.Run(text, func(t *testing.T) {
			s := NewServer(Config{})
			c := session(t, s)
			call(t, s, c, "POST", "/api/import", testProject)
			_, before := call(t, s, c, "GET", "/api/export", "")
			raw, _ := json.Marshal(map[string]string{"text": text})
			r, data := call(t, s, c, "POST", "/api/message", string(raw))
			if data["preview"] != nil || (r.Code == 200 && data["status"] != "unsupported") {
				t.Fatalf("unsafe fallback: %s", r.Body.String())
			}
			feedback, _ := data["message"].(string)
			if r.Code != 200 {
				feedback, _ = data["error"].(string)
			}
			if !strings.Contains(feedback, "未执行任何修改") || !strings.Contains(feedback, "选中的片段") {
				t.Errorf("fallback lacks state assurance or actionable guidance: %s", feedback)
			}
			_, after := call(t, s, c, "GET", "/api/export", "")
			if !reflect.DeepEqual(before, after) {
				t.Fatal("fallback changed project content or revision")
			}
			r, _ = call(t, s, c, "POST", "/api/undo", `{"request_id":"no-commit"}`)
			if r.Code != 409 {
				t.Fatal("fallback created undo history")
			}
			// A corrected instruction must still work without reloading the project.
			r, data = call(t, s, c, "POST", "/api/message", `{"text":"把 a 缩短 2 秒"}`)
			if r.Code != 200 || data["status"] != "plan" {
				t.Fatalf("cannot recover after fallback: %s", r.Body.String())
			}
		})
	}
}
