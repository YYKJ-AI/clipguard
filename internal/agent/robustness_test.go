package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestErrorPresentationUsesCodes(t *testing.T) {
	for code, message := range errorMessages {
		err := fmt.Errorf("wrapped: %w", appError(code, "private-project-id internal-field"))
		if publicError(err) != message {
			t.Errorf("code %s has no presentation", code)
		}
	}
	if strings.Contains(publicError(errors.New("private-project-id")), "private") {
		t.Fatal("unknown diagnostic leaked")
	}
}

func TestLegacyRequestHasCorrelationID(t *testing.T) {
	r, _ := call(t, NewServer(Config{}), nil, "GET", "/api/state", "")
	if r.Header().Get("X-Request-ID") == "" {
		t.Fatal("request cannot be correlated with logs")
	}
}

func TestHistoryClockIsDeterministic(t *testing.T) {
	now := time.Date(2026, 11, 12, 1, 2, 3, 0, time.UTC)
	w := newWorkspace(func() time.Time { return now })
	if err := w.Load(fixture(t)); err != nil {
		t.Fatal(err)
	}
	_, b, _ := w.Snapshot()
	p, err := w.Preview(b, []Operation{{Type: "delete", TargetID: "a"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Confirm(p.ID); err != nil {
		t.Fatal(err)
	}
	if got := w.History().Entries[0].Time; got != now.Format(time.RFC3339) {
		t.Fatalf("time=%s", got)
	}
}

func TestRegistryDocumentsEveryOperation(t *testing.T) {
	for kind, spec := range operationRegistry {
		if spec.apply == nil || !strings.Contains(operationDescriptions(), kind) || !strings.Contains(capabilityDescription(), spec.description) {
			t.Fatalf("operation %s missing from contract", kind)
		}
	}
}

func scaleProject(n int) Project {
	p := Project{ID: "scale", Revision: 1, Tracks: []Track{{ID: "video", Kind: "video"}}, Clips: make([]Clip, n)}
	for i := range p.Clips {
		p.Clips[i] = Clip{ID: fmt.Sprint(i), TrackID: "video", StartMS: int64(i) * 10, DurationMS: 5, SourceInMS: int64ptr(0), SourceDurationMS: int64ptr(10)}
	}
	return p
}

func TestProjectClipLimitBoundary(t *testing.T) {
	p := scaleProject(4096)
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeProject(raw); err != nil {
		t.Fatalf("4096 clips rejected: %v", err)
	}
	p.Clips = append(p.Clips, Clip{ID: "overflow"})
	if err = Validate(p); err == nil {
		t.Fatal("4097 clips accepted")
	}
}

func BenchmarkApply(b *testing.B) {
	p := scaleProject(4096)
	ops := []Operation{{Type: "trim", TargetID: "0", DurationMS: int64ptr(4)}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := Apply(p, ops); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkValidate(b *testing.B) {
	p := scaleProject(4096)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := Validate(p); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkDiff(b *testing.B) {
	before := scaleProject(4096)
	after := Clone(before)
	after.Clips[0].DurationMS = 4
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if changes := Diff(before, after); len(changes) != 1 {
			b.Fatal("incorrect difference")
		}
	}
}

func FuzzOfflineParser(f *testing.F) {
	for _, seed := range []string{"把 a 缩短 1 秒", "删除这段", `把 s 的字幕改为"分号；字幕"`, "不要删除 a，只把它缩短 1 秒", "把 a 缩短 999999999999999999999 秒", "\xff\x00", "\"\\"} {
		f.Add(seed)
	}
	f.Add(strings.Repeat("\x80", 2432))
	f.Add(strings.Repeat("中", 2000))
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 16384 {
			t.Skip()
		}
		p := fixture(t)
		before := Clone(p)
		result, err := (OfflineParser{}).Parse(context.Background(), p, []Turn{{Role: "user", Content: text}})
		if !reflect.DeepEqual(p, before) {
			t.Fatal("parser mutated project")
		}
		if err == nil {
			if err := validateParseResult(p, result); err != nil {
				t.Fatalf("parser returned invalid result: %v", err)
			}
		}
	})
}
