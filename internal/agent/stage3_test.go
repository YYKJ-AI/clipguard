package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fixedStage3Parser struct{ result ParseResult }

func (p fixedStage3Parser) Parse(context.Context, Project, []Turn) (ParseResult, error) {
	return p.result, nil
}

func TestStage3CapabilityGuardRespectsQuotedTextAndNegation(t *testing.T) {
	for _, input := range []string{
		`把 s 的字幕改为“检查异常画面”`,
		`不要检查异常画面，只把 a 缩短 1 秒`,
	} {
		result := ParseResult{Status: "plan", Operations: []Operation{{Type: "trim", TargetID: "a", DurationMS: int64ptr(4000)}}}
		s := NewServer(Config{Model: fixedStage3Parser{result}})
		cookie := session(t, s)
		call(t, s, cookie, "POST", "/api/import", testProject)
		body, _ := json.Marshal(map[string]string{"mode": "model", "text": input})
		rec, data := call(t, s, cookie, "POST", "/api/message", string(body))
		if rec.Code != 200 || data["status"] != "plan" {
			t.Fatalf("guard mistook data or negation for an action: %s", rec.Body.String())
		}
	}
}

func TestStage3UnsupportedMediaRequestCannotBecomePlanOrClarification(t *testing.T) {
	for _, input := range []string{
		"检查视频中的异常纯色画面，将不正常的画面片段去掉",
		"检查视频中的花屏、噪点等异常画面并处理",
		"修复异常画面，但不要删除对应时间段的旁白",
	} {
		for _, wrong := range []ParseResult{
			{Status: "clarify", Message: "请给出位置"},
			{Status: "plan", Operations: []Operation{{Type: "delete", TargetID: "a"}}},
		} {
			s := NewServer(Config{Model: fixedStage3Parser{wrong}})
			cookie := session(t, s)
			call(t, s, cookie, "POST", "/api/import", testProject)
			rec, data := call(t, s, cookie, "POST", "/api/message", `{"mode":"model","text":"`+input+`"}`)
			if rec.Code != 200 || data["status"] != "unsupported" || !strings.Contains(data["message"].(string), "暂时不支持") || data["preview"] != nil {
				t.Fatalf("unsupported media request leaked through: %s", rec.Body.String())
			}
		}
	}
}

func TestStage3CapabilityConsultationDistinguishesProjectExport(t *testing.T) {
	for _, question := range []string{"能导出吗？", "你有哪些功能？"} {
		s := NewServer(Config{Model: fixedStage3Parser{ParseResult{Status: "answer", Message: "不支持导出"}}})
		cookie := session(t, s)
		body, _ := json.Marshal(map[string]string{"mode": "model", "text": question})
		rec, data := call(t, s, cookie, "POST", "/api/message", string(body))
		message, _ := data["message"].(string)
		if rec.Code != 200 || data["status"] != "answer" || !strings.Contains(message, "工程 JSON") || !strings.Contains(message, "视频") {
			t.Fatalf("export capability was misstated: %s", rec.Body.String())
		}
	}
}

func TestStage3PublicErrorsHideParserAndInternalIdentifiers(t *testing.T) {
	s := NewServer(Config{})
	cookie := session(t, s)
	for _, request := range []struct{ path, body string }{
		{"/api/import", `[]`},
		{"/api/message", `{"text":[]}`},
	} {
		rec, data := call(t, s, cookie, "POST", request.path, request.body)
		text, _ := data["error"].(string)
		if rec.Code < 400 || strings.Contains(text, "unmarshal") || strings.Contains(text, "agent.") || strings.Contains(text, "json:") {
			t.Fatalf("internal error leaked: %s", rec.Body.String())
		}
	}
	call(t, s, cookie, "POST", "/api/import", testProject)
	rec, data := call(t, s, cookie, "POST", "/api/message", `{"text":"把 l 的字幕改为“新”"}`)
	text, _ := data["error"].(string)
	if rec.Code != 422 || !strings.Contains(text, "锁定") || strings.Contains(text, "locked") || strings.Contains(text, "片段 ID") {
		t.Fatalf("unfriendly edit rejection: %s", rec.Body.String())
	}
}

func TestStage3SelectedCaptionDeleteStillNeedsPreview(t *testing.T) {
	s := NewServer(Config{})
	cookie := session(t, s)
	call(t, s, cookie, "POST", "/api/import", testProject)
	rec, data := call(t, s, cookie, "POST", "/api/message", `{"text":"删除这段","selected_clip_id":"s"}`)
	if rec.Code != 200 || data["status"] != "plan" {
		t.Fatalf("caption example cannot resolve its explicit selection: %s", rec.Body.String())
	}
	_, exported := call(t, s, cookie, "GET", "/api/export", "")
	if exported["revision"] != float64(1) {
		t.Fatal("caption deletion was executed without confirmation")
	}
}
