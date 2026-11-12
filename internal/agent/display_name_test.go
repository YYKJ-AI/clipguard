package agent

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func namedProjectJSON(nameJSON string) string {
	return strings.Replace(testProject, `"id":"a",`, `"id":"a","display_name":`+nameJSON+`,`, 1)
}

// Catch missing support, byte-based length limits, and silent field loss.
func TestDisplayNameImportExport(t *testing.T) {
	for _, tc := range []struct{ name, raw, want string }{
		{"legacy", testProject, ""},
		{"named", namedProjectJSON(`"开场镜头"`), `"display_name":"开场镜头"`},
		{"empty", namedProjectJSON(`""`), `"display_name":""`},
		{"unicode boundary", namedProjectJSON(`"` + strings.Repeat("镜", 128) + `"`), `"display_name":"` + strings.Repeat("镜", 128) + `"`},
		{"emoji boundary", namedProjectJSON(`"` + strings.Repeat("🎬", 128) + `"`), `"display_name":"` + strings.Repeat("🎬", 128) + `"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := DecodeProject([]byte(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			exported, err := json.Marshal(Clone(p))
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if strings.Contains(string(exported), `"display_name"`) {
					t.Fatal("legacy project gained name fields")
				}
			} else if !strings.Contains(string(exported), tc.want) {
				t.Fatalf("name lost on clone/export: %s", exported)
			}
		})
	}
}

// Invalid imports must preserve both current project and an existing preview.
func TestInvalidDisplayNameImportPreservesPlan(t *testing.T) {
	for _, raw := range []string{`null`, `42`, `true`, `[]`, `{}`, `"` + strings.Repeat("镜", 129) + `"`} {
		t.Run(raw, func(t *testing.T) {
			w := loaded(t)
			preview := previewTrim(t, w, 3000)
			before, _, _ := w.Snapshot()
			p, err := DecodeProject([]byte(namedProjectJSON(raw)))
			if err == nil {
				err = w.Load(p)
			}
			if err == nil {
				t.Fatal("invalid name accepted")
			}
			after, _, _ := w.Snapshot()
			if !reflect.DeepEqual(before, after) {
				t.Fatal("invalid import mutated project")
			}
			if _, err := w.Confirm(preview.ID); err != nil {
				t.Fatalf("invalid import consumed preview: %v", err)
			}
		})
	}
}

// The optional label must not relax other required fields or operation fields.
func TestDisplayNameKeepsStrictBoundaries(t *testing.T) {
	valid := namedProjectJSON(`"开场"`)
	for _, raw := range []string{
		strings.Replace(valid, `"start_ms":0,`, "", 1),
		strings.Replace(valid, `"display_name":"开场"`, `"display_name":"开场","extra":true`, 1),
	} {
		if _, err := DecodeProject([]byte(raw)); err == nil {
			t.Fatal("name relaxed strict project validation")
		}
	}
	var ops []Operation
	if err := strictJSON([]byte(`[{"type":"delete","target_id":"a","display_name":"开场"}]`), &ops); err == nil {
		t.Fatal("operation whitelist accepts display_name")
	}
}

func TestNamedProjectEditingAndUndo(t *testing.T) {
	p, err := DecodeProject([]byte(namedProjectJSON(`"同名镜头"`)))
	if err != nil {
		t.Fatal(err)
	}
	// Duplicate labels are allowed; stable IDs remain the editing authority.
	raw, _ := json.Marshal(p)
	raw = []byte(strings.Replace(string(raw), `"id":"b",`, `"id":"b","display_name":"同名镜头",`, 1))
	p, err = DecodeProject(raw)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWorkspace()
	if err := w.Load(p); err != nil {
		t.Fatal(err)
	}
	preview := previewTrim(t, w, 3000)
	result, err := w.Confirm(preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Project.Clips[0].DurationMS != 3000 || result.Project.Clips[1].DurationMS != 3000 || len(result.Changes) != 1 {
		t.Fatal("duplicate labels changed target identity")
	}
	exported, _ := json.Marshal(result.Project)
	if strings.Count(string(exported), `"display_name":"同名镜头"`) != 2 {
		t.Fatal("edit dropped labels")
	}
	result, err = w.Undo("name-undo")
	if err != nil {
		t.Fatal(err)
	}
	result.Project.Revision = p.Revision
	if !reflect.DeepEqual(result.Project, p) {
		t.Fatal("undo did not restore labels and content")
	}
}

func TestNamedProjectHTTPRoundTrip(t *testing.T) {
	s := NewServer(Config{})
	c := session(t, s)
	rec, _ := call(t, s, c, http.MethodPost, "/api/import", namedProjectJSON(`"开场镜头"`))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	rec, _ = call(t, s, c, http.MethodGet, "/api/export", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"display_name":"开场镜头"`) {
		t.Fatalf("HTTP round-trip lost label: %s", rec.Body.String())
	}
}
