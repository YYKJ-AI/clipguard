package agent

import (
	"reflect"
	"testing"
)

func TestRelativeTrimUsesEngineWorkingState(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want int64
	}{
		{"one second", `[{"type":"trim","target_id":"a","shorten_by_ms":1000}]`, 4000},
		{"two relative steps", `[{"type":"trim","target_id":"a","shorten_by_ms":1000},{"type":"trim","target_id":"a","shorten_by_ms":1000}]`, 3000},
		{"absolute then relative", `[{"type":"trim","target_id":"a","duration_ms":4000},{"type":"trim","target_id":"a","shorten_by_ms":500}]`, 3500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := fixture(t)
			before := Clone(original)
			next, changes, err := Apply(original, operations(t, tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			if next.Clips[0].DurationMS != tc.want || len(changes) != 1 || *next.Clips[0].SourceInMS != 1000 || next.Clips[0].StartMS != 0 {
				t.Fatalf("wrong relative trim: %+v", next.Clips[0])
			}
			if !reflect.DeepEqual(original, before) {
				t.Fatal("relative trim mutated its input")
			}
		})
	}
}

func TestRelativeTrimInvalidBatchIsAtomic(t *testing.T) {
	for _, raw := range []string{
		`[{"type":"trim","target_id":"a","shorten_by_ms":0}]`,
		`[{"type":"trim","target_id":"a","shorten_by_ms":-1}]`,
		`[{"type":"trim","target_id":"a","shorten_by_ms":5000}]`,
		`[{"type":"trim","target_id":"a","shorten_by_ms":9223372036854775807}]`,
		`[{"type":"trim","target_id":"a","shorten_by_ms":1000,"duration_ms":3000}]`,
		`[{"type":"move","target_id":"a","start_ms":0,"shorten_by_ms":1000}]`,
		`[{"type":"caption","target_id":"s","text":"文字","shorten_by_ms":1000}]`,
		`[{"type":"delete","target_id":"a","shorten_by_ms":1000}]`,
		`[{"type":"trim","target_id":"s","shorten_by_ms":1000}]`,
		`[{"type":"trim","target_id":"a","shorten_by_ms":1000},{"type":"delete","target_id":"l"}]`,
	} {
		t.Run(raw, func(t *testing.T) {
			p := fixture(t)
			before := Clone(p)
			var ops []Operation
			if err := strictJSON([]byte(raw), &ops); err == nil {
				if _, _, err := Apply(p, ops); err == nil {
					t.Fatal("invalid relative operation accepted")
				}
			}
			if !reflect.DeepEqual(p, before) {
				t.Fatal("invalid relative batch mutated input")
			}
		})
	}
}
