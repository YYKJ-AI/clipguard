package agent

import (
	"errors"
	"testing"
)

func TestEngineErrorsHaveStableCodes(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		op         Operation
	}{
		{"locked", "TRACK_LOCKED", Operation{Type: "delete", TargetID: "l"}},
		{"overlap", "OVERLAP", Operation{Type: "move", TargetID: "b", StartMS: int64ptr(0)}},
		{"missing", "TARGET_NOT_FOUND", Operation{Type: "delete", TargetID: "private-project-id"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Apply(fixture(t), []Operation{tc.op})
			var coded interface{ ErrorCode() string }
			if !errors.As(err, &coded) || coded.ErrorCode() != tc.code {
				t.Fatalf("missing stable code: %v", err)
			}
		})
	}
}
