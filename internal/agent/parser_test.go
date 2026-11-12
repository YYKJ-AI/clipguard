package agent

import (
	"context"
	"testing"
)

// Detect accidental acceptance of negation, dropped clauses, and splitting inside caption text.
func TestOfflineParsingAndQuotedSeparators(t *testing.T) {
	p := fixture(t)
	parser := OfflineParser{}
	result, err := parser.Parse(context.Background(), p, []Turn{{Role: "user", Content: `把 a 缩短 2 秒；把 s 的字幕改为“再见；不要删除”`}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "plan" || len(result.Operations) != 2 || *result.Operations[0].DurationMS != 3000 || *result.Operations[1].Text != "再见；不要删除" {
		t.Fatalf("wrong parse %+v", result)
	}
	for _, text := range []string{"不要删除 a", "删除 a 并运行脚本", "把 a 缩短两秒", "剪得紧凑一点"} {
		result, err = parser.Parse(context.Background(), p, []Turn{{Role: "user", Content: text}})
		if err == nil && result.Status == "plan" {
			t.Fatalf("unsupported accepted: %s", text)
		}
	}
}
func TestOfflineAmbiguity(t *testing.T) {
	r, err := (OfflineParser{}).Parse(context.Background(), fixture(t), []Turn{{Role: "user", Content: "把第二段移到开头"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "clarify" || len(r.Candidates) != 2 || r.Reference != "第二段" {
		t.Fatalf("missing candidates %+v", r)
	}
}
func TestOfflineMoveAndSubtitleTime(t *testing.T) {
	for _, text := range []string{"把 b 移到 v2 轨道的 10 秒处", "把 s 的时间改为从 3 秒到 5 秒"} {
		r, err := (OfflineParser{}).Parse(context.Background(), fixture(t), []Turn{{Role: "user", Content: text}})
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != "plan" {
			t.Fatalf("not parsed: %s: %+v", text, r)
		}
		if _, _, err := Apply(fixture(t), r.Operations); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRepeatedRelativeTrimUsesWorkingState(t *testing.T) {
	p := fixture(t)
	r, err := (OfflineParser{}).Parse(context.Background(), p, []Turn{{Role: "user", Content: "把 a 缩短 1 秒；把 a 缩短 1 秒"}})
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := Apply(p, r.Operations)
	if err != nil {
		t.Fatal(err)
	}
	if got.Clips[0].DurationMS != 3000 {
		t.Fatalf("relative operations not sequential: %d", got.Clips[0].DurationMS)
	}
}
