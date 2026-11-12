package agent

import (
	"testing"
	"time"
)

func TestPreviewOwnsOperationData(t *testing.T) {
	w := NewWorkspace()
	if err := w.Load(fixture(t)); err != nil {
		t.Fatal(err)
	}
	_, binding, _ := w.Snapshot()
	text := "planned"
	ops := []Operation{{Type: "caption", TargetID: "s", Text: &text}}
	preview, err := w.Preview(binding, ops, "")
	if err != nil {
		t.Fatal(err)
	}
	text = "external mutation"
	ops[0].TargetID = "a"
	*preview.Operations[0].Text = "response mutation"
	result, err := w.Confirm(preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range result.Project.Clips {
		if c.ID == "s" && *c.Text != "planned" {
			t.Fatalf("plan changed: %s", *c.Text)
		}
	}
}

func TestHistoryOwnsChangeData(t *testing.T) {
	w := NewWorkspace()
	if err := w.Load(fixture(t)); err != nil {
		t.Fatal(err)
	}
	_, binding, _ := w.Snapshot()
	preview, err := w.Preview(binding, []Operation{{Type: "delete", TargetID: "a"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Confirm(preview.ID); err != nil {
		t.Fatal(err)
	}
	history := w.History()
	*history.Entries[0].Changes[0].Before.SourceInMS = 999
	if *w.History().Entries[0].Changes[0].Before.SourceInMS == 999 {
		t.Fatal("history exposed internal state")
	}
}

func TestSessionLifecycleExpiryAndTouch(t *testing.T) {
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	s := NewServer(Config{Now: func() time.Time { return now }, SessionIdleTTL: time.Minute})
	c := session(t, s)
	now = now.Add(45 * time.Second)
	r, _ := call(t, s, c, "GET", "/api/state", "")
	if r.Code != 200 {
		t.Fatal("live session expired")
	}
	now = now.Add(45 * time.Second)
	r, _ = call(t, s, c, "GET", "/api/state", "")
	if r.Code != 200 {
		t.Fatal("touch did not extend idle time")
	}
	now = now.Add(time.Minute)
	r, _ = call(t, s, c, "GET", "/api/state", "")
	if r.Code != 401 || len(s.sessions) != 0 {
		t.Fatal("expired session retained")
	}
	for i := 0; i < sessionCreationBurst; i++ {
		session(t, s)
	}
	r, _ = call(t, s, nil, "POST", "/api/session", `{}`)
	if r.Code != 429 {
		t.Fatal("rate limit not reached")
	}
	now = now.Add(time.Minute)
	session(t, s)
}
