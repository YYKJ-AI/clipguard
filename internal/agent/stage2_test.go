package agent

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func parseStage2(t *testing.T, p Project, parser OfflineParser, instruction string) ParseResult {
	t.Helper()
	before := Clone(p)
	result, err := parser.Parse(context.Background(), p, []Turn{{Role: "user", Content: instruction}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p, before) {
		t.Fatal("parsing mutated project")
	}
	return result
}

func TestStage2SelectedReferences(t *testing.T) {
	p := fixture(t)
	tests := []struct{ name, input, selected, wantStatus, wantID string }{
		{"this clip without selection", "把这段缩短 1 秒", "", "clarify", ""},
		{"this clip selected", "把这段缩短 1 秒", "a", "plan", "a"},
		{"recent caption without context", "把刚刚那个字幕改为“再见”", "", "clarify", ""},
		{"recent caption selected", "把刚刚那个字幕改为“再见”", "s", "plan", "s"},
		{"recent caption ignores selected video", "把刚刚那个字幕改为“再见”", "a", "clarify", ""},
		{"wrong kind selected", "把这段缩短 1 秒", "s", "clarify", ""},
		{"second shot multiple tracks", "把第二个镜头缩短 1 秒", "", "clarify", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseStage2(t, p, OfflineParser{selectedID: tc.selected}, tc.input)
			if result.Status != tc.wantStatus {
				t.Fatalf("status=%q message=%q", result.Status, result.Message)
			}
			if tc.wantID != "" && (len(result.Operations) != 1 || result.Operations[0].TargetID != tc.wantID) {
				t.Fatalf("operations=%+v", result.Operations)
			}
			if tc.wantStatus == "clarify" && len(result.Operations) > 0 {
				t.Fatal("ambiguous request generated edits")
			}
		})
	}
}

func TestStage2UniqueAndDuplicateNames(t *testing.T) {
	p := fixture(t)
	name := "开场镜头"
	p.Clips[0].DisplayName = &name
	result := parseStage2(t, p, OfflineParser{}, "把开场镜头缩短 1 秒")
	if result.Status != "plan" || result.Operations[0].TargetID != "a" {
		t.Fatalf("unique name: %+v", result)
	}
	p.Clips[2].DisplayName = &name
	result = parseStage2(t, p, OfflineParser{}, "把开场镜头缩短 1 秒")
	if result.Status != "clarify" || len(result.Candidates) != 2 || result.Candidates[0].ID != "a" || result.Candidates[1].ID != "c" {
		t.Fatalf("duplicate names guessed: %+v", result)
	}
}

func TestStage2RecentCaptionSurvivesUnrelatedVideoSelection(t *testing.T) {
	p := fixture(t)
	result := parseStage2(t, p, OfflineParser{selectedID: "a", lastCaptionID: "s"}, "把刚刚那个字幕改为“再见”")
	if result.Status != "plan" || len(result.Operations) != 1 || result.Operations[0].TargetID != "s" {
		t.Fatalf("recent caption lost to selected video: %+v", result)
	}
}

func TestStage2MoveIntoExplicitGap(t *testing.T) {
	p := fixture(t)
	p.Clips[1].StartMS = 10000
	result := parseStage2(t, p, OfflineParser{}, "把 d 移到 a 和 b 之间")
	if result.Status != "plan" || len(result.Operations) != 1 {
		t.Fatalf("no plan: %+v", result)
	}
	op := result.Operations[0]
	if op.TargetID != "d" || op.StartMS == nil || *op.StartMS != 5000 || op.TrackID == nil || *op.TrackID != "v1" {
		t.Fatalf("wrong gap: %+v", op)
	}
	next, _, err := Apply(p, result.Operations)
	if err != nil || next.Clips[3].StartMS != 5000 || next.Clips[3].TrackID != "v1" {
		t.Fatalf("gap not executable: %+v %v", next.Clips, err)
	}
	// Original sample has only a one-second gap, too short for clip d.
	original := fixture(t)
	rejected, err := OfflineParser{}.Parse(context.Background(), original, []Turn{{Role: "user", Content: "把 d 移到 a 和 b 之间"}})
	if err == nil && rejected.Status == "plan" {
		t.Fatal("insufficient gap turned into plan")
	}
	result = parseStage2(t, p, OfflineParser{}, "在 a 和 b 之间添加转场")
	if result.Status == "plan" {
		t.Fatal("unsupported insertion was silently converted to move")
	}
}

func TestStage2NegationKeepsOnlyPositiveRequest(t *testing.T) {
	p := fixture(t)
	result := parseStage2(t, p, OfflineParser{}, "不要删除 a，只把它缩短 1 秒")
	if result.Status != "plan" || len(result.Operations) != 1 || result.Operations[0].Type != "trim" || result.Operations[0].TargetID != "a" || result.Operations[0].DurationMS == nil || *result.Operations[0].DurationMS != 4000 {
		t.Fatalf("negation generated wrong edit: %+v", result)
	}
	result = parseStage2(t, p, OfflineParser{}, "不要删除 a")
	if result.Status == "plan" {
		t.Fatal("negative-only request generated plan")
	}
	result = parseStage2(t, p, OfflineParser{}, "把 a 缩短 1 秒；删除 l")
	if result.Status != "plan" {
		t.Fatalf("no combined candidate plan: %+v", result)
	}
	before := Clone(p)
	if _, _, err := Apply(p, result.Operations); err == nil || !strings.Contains(err.Error(), "锁定") {
		t.Fatalf("locked second operation accepted: %v", err)
	}
	if !reflect.DeepEqual(before, p) {
		t.Fatal("failed batch mutated source project")
	}
}
