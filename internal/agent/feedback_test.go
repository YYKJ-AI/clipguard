package agent

import (
	"errors"
	"strings"
	"testing"
)

// The "no change was made" sentence belongs to instructionFeedback only. If a
// code-level message repeats it, users see the same sentence twice.
func TestInstructionFeedbackStatesReasonOnce(t *testing.T) {
	const tail = "本条指令未执行任何修改"
	for _, code := range []ErrorCode{
		CodeJSON, CodeLocked, CodeOverlap, CodeTarget, CodeTrack,
		CodeTime, CodeProject, CodeModel, CodeInput, CodeConflict,
	} {
		text := instructionFeedback(appError(code, "internal detail"))
		if got := strings.Count(text, tail); got != 1 {
			t.Fatalf("%s: tail appears %d times: %s", code, got, text)
		}
		if strings.Contains(text, "internal detail") {
			t.Fatalf("%s: internal detail leaked to user text: %s", code, text)
		}
	}
}

// A track that is missing or of the wrong kind is an edit problem. Telling the
// user to inspect the project file sends them to the wrong place.
func TestTrackProblemsAreEditProblems(t *testing.T) {
	caption, missing := "c1", "no-such-track"
	for _, tc := range []struct {
		name   string
		track  string
		detail string
	}{
		{"wrong kind", caption, "字幕轨道"},
		{"unknown track", missing, "不存在"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Apply(fixture(t), []Operation{{
				Type:     "move",
				TargetID: "a",
				StartMS:  int64ptr(0),
				TrackID:  &tc.track,
			}})
			var coded interface{ ErrorCode() string }
			if !errors.As(err, &coded) || coded.ErrorCode() != "TRACK_INVALID" {
				t.Fatalf("expected TRACK_INVALID, got %v", err)
			}
			message := publicError(err)
			if !strings.Contains(message, tc.detail) {
				t.Fatalf("message lacks the actual cause: %s", message)
			}
			if strings.Contains(message, "检查工程文件") || strings.Contains(message, "工程格式") {
				t.Fatalf("edit problem blamed on the project file: %s", message)
			}
		})
	}
}

// Extra or null fields are malformed instructions, not malformed projects.
func TestOperationFieldErrorsAreInputProblems(t *testing.T) {
	for _, raw := range []string{
		`[{"type":"delete","target_id":"a","start_ms":0}]`,
		`[{"type":"delete","target_id":null}]`,
	} {
		var ops []Operation
		err := strictJSON([]byte(raw), &ops)
		var coded interface{ ErrorCode() string }
		if !errors.As(err, &coded) || coded.ErrorCode() != "INVALID_INPUT" {
			t.Fatalf("%s: expected INVALID_INPUT, got %v", raw, err)
		}
	}
}
