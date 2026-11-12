package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

type contextParser struct {
	seen       [][]Turn
	failSecond bool
}

func (p *contextParser) Parse(_ context.Context, _ Project, turns []Turn) (ParseResult, error) {
	p.seen = append(p.seen, append([]Turn(nil), turns...))
	if len(p.seen) == 2 {
		if p.failSecond {
			return ParseResult{}, errors.New("temporary model error")
		}
		return ParseResult{Status: "answer", Message: "请继续选择原候选"}, nil
	}
	return ParseResult{Status: "clarify", Message: "请选择片段", Candidates: []Candidate{{ID: "a", Label: "片段 a"}, {ID: "b", Label: "片段 b"}}}, nil
}

func TestSessionLifecycleCapacity(t *testing.T) {
	s := NewServer(Config{MaxSessions: 2})
	first := session(t, s)
	session(t, s)
	session(t, s)
	r, _ := call(t, s, first, "GET", "/api/state", "")
	if r.Code != 401 {
		t.Fatalf("evicted session status=%d", r.Code)
	}
}

func TestSessionLifecycleRateLimit(t *testing.T) {
	s := NewServer(Config{})
	limited := false
	for i := 0; i < 30; i++ {
		r, _ := call(t, s, nil, "POST", "/api/session", `{}`)
		if r.Code == 429 {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("anonymous session creation is unbounded")
	}
}

func TestClarifyContextIsJSON(t *testing.T) {
	parser := &contextParser{}
	s := NewServer(Config{Model: parser})
	c := session(t, s)
	call(t, s, c, "POST", "/api/import", testProject)
	call(t, s, c, "POST", "/api/message", `{"text":"删除第二段","mode":"model"}`)
	call(t, s, c, "POST", "/api/message", `{"text":"a 那个","mode":"model"}`)
	turns := parser.seen[1]
	if len(turns) < 3 {
		t.Fatalf("lost original context: %+v", turns)
	}
	var payload struct {
		Message    string      `json:"message"`
		Candidates []Candidate `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(turns[1].Content), &payload); err != nil || len(payload.Candidates) != 2 {
		t.Fatalf("not structured clarification: %s (%v)", turns[1].Content, err)
	}
}

func TestFreeTextKeepsClarification(t *testing.T) {
	for _, failSecond := range []bool{false, true} {
		t.Run(fmt.Sprint(failSecond), func(t *testing.T) {
			parser := &contextParser{}
			parser.failSecond = failSecond
			s := NewServer(Config{Model: parser})
			c := session(t, s)
			call(t, s, c, "POST", "/api/import", testProject)
			_, data := call(t, s, c, "POST", "/api/message", `{"text":"删除第二段","mode":"model"}`)
			id := data["clarification_id"].(string)
			call(t, s, c, "POST", "/api/message", `{"text":"a 那个","mode":"model"}`)
			r, _ := call(t, s, c, "POST", "/api/message", fmt.Sprintf(`{"choice":"a","clarification_id":%q,"mode":"model"}`, id))
			if r.Code != 200 {
				t.Fatalf("original candidate expired: %s", r.Body.String())
			}
		})
	}
}

func TestTextLimitUnified(t *testing.T) {
	s := NewServer(Config{})
	c := session(t, s)
	call(t, s, c, "POST", "/api/import", testProject)
	for _, n := range []int{2000, 2001} {
		r, _ := call(t, s, c, "POST", "/api/message", fmt.Sprintf(`{"text":%q}`, strings.Repeat("中", n)))
		if n == 2000 && r.Code == 400 {
			t.Fatalf("2000 characters rejected: %s", r.Body.String())
		}
		if n == 2001 && (r.Code != 400 || !strings.Contains(r.Body.String(), "2000")) {
			t.Fatalf("limit not explicit: %s", r.Body.String())
		}
	}
}

func TestLargeProjectHasCandidates(t *testing.T) {
	p := Project{ID: "large", Revision: 1, Tracks: []Track{{ID: "video", Kind: "video"}}, Clips: []Clip{}}
	for i := 0; i < 70; i++ {
		p.Clips = append(p.Clips, Clip{ID: fmt.Sprint(i), TrackID: "video", StartMS: int64(i * 10), DurationMS: 5, SourceInMS: int64ptr(0), SourceDurationMS: int64ptr(10)})
	}
	result, err := (OfflineParser{}).Parse(context.Background(), p, []Turn{{"user", "删除这段"}})
	if err != nil || result.Status != "clarify" || len(result.Candidates) == 0 {
		t.Fatalf("dead-end clarification: %+v %v", result, err)
	}
}

func TestCopiesDoNotAlias(t *testing.T) {
	p, err := DecodeProject([]byte(testProject))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("clone", func(t *testing.T) {
		cp := Clone(p)
		*cp.Clips[0].SourceInMS = 7
		if *p.Clips[0].SourceInMS == 7 {
			t.Fatal("clone aliases original")
		}
	})
	t.Run("insert", func(t *testing.T) {
		source, track := "a", "v1"
		after, _, err := Apply(p, []Operation{{Type: "insert", TargetID: "copy", SourceID: &source, TrackID: &track, StartMS: int64ptr(30000)}})
		if err != nil {
			t.Fatal(err)
		}
		*after.Clips[len(after.Clips)-1].SourceInMS = 8
		if *after.Clips[0].SourceInMS == 8 {
			t.Fatal("insert aliases source")
		}
	})
	t.Run("caption operation", func(t *testing.T) {
		text := "new"
		after, _, err := Apply(p, []Operation{{Type: "caption", TargetID: "s", Text: &text}})
		if err != nil {
			t.Fatal(err)
		}
		text = "mutated"
		for _, c := range after.Clips {
			if c.ID == "s" && *c.Text != "new" {
				t.Fatal("result aliases caller operation")
			}
		}
	})
}

func TestFlushEmptyProductResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	NewServer(Config{}).flushProduct(rec, newProductCapture())
	if rec.Code != 200 {
		t.Fatalf("status=%d", rec.Code)
	}
}
