package agent

import (
	"context"
	"strings"
	"testing"
)

type technicalClarificationParser struct{}

func (technicalClarificationParser) Parse(context.Context, Project, []Turn) (ParseResult, error) {
	return ParseResult{Status: "clarify", Message: "请确认您要删除的是轨道 v2 上 ID 为 'c'、从 0 到 4000 毫秒的片段吗？还是只缩短它？", Candidates: []Candidate{{ID: "c", Label: "v2 / c 0—4000 ms"}}}, nil
}

// A model's technical wording must not reach the customer, and presenting a
// clarification must neither lose the question nor change its stable target.
func TestClarificationUsesCustomerLanguageWithoutChangingTarget(t *testing.T) {
	s := NewServer(Config{Model: technicalClarificationParser{}})
	cookie := session(t, s)
	call(t, s, cookie, "POST", "/api/import", testProject)
	r, data := call(t, s, cookie, "POST", "/api/message", `{"mode":"model","text":"轨道二第一段适配删掉"}`)
	if r.Code != 200 || data["status"] != "clarify" {
		t.Fatal(r.Body.String())
	}
	message := data["message"].(string)
	for _, raw := range []string{"v2", "'c'", "4000", "毫秒", "ID"} {
		if strings.Contains(message, raw) {
			t.Fatalf("technical wording leaked: %s", message)
		}
	}
	if !strings.Contains(message, "4 秒") || !strings.Contains(message, "还是只缩短它？") {
		t.Fatalf("question or readable time lost: %s", message)
	}
	candidate := data["candidates"].([]any)[0].(map[string]any)
	if candidate["id"] != "c" {
		t.Fatal("target changed")
	}
	if strings.Contains(candidate["label"].(string), "ms") {
		t.Fatal("technical candidate leaked")
	}
	_, exported := call(t, s, cookie, "GET", "/api/export", "")
	if exported["revision"] != float64(1) || len(exported["clips"].([]any)) != 6 {
		t.Fatal("clarification edited project")
	}
}
