package agent

import (
	"context"
	"errors"
	"testing"
)

func clipByID(t *testing.T, p Project, id string) Clip {
	t.Helper()
	for _, c := range p.Clips {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("clip %s missing", id)
	return Clip{}
}

// Swapping two clips can only pass through a moment where they overlap. The
// final state must still be overlap-free, but the intermediate one may not be.
func TestBatchCanSwapOrder(t *testing.T) {
	p := fixture(t)
	ops := []Operation{
		{Type: "move", TargetID: "b", StartMS: int64ptr(0)},
		{Type: "move", TargetID: "a", StartMS: int64ptr(6000)},
	}
	next, changes, err := Apply(p, ops)
	if err != nil {
		t.Fatalf("swap rejected: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("expected two changed clips, got %d", len(changes))
	}
	if got := clipByID(t, next, "b").StartMS; got != 0 {
		t.Fatalf("b start=%d", got)
	}
	if got := clipByID(t, next, "a").StartMS; got != 6000 {
		t.Fatalf("a start=%d", got)
	}
	if err := Validate(next); err != nil {
		t.Fatalf("committed state invalid: %v", err)
	}
	// The caller's project must stay untouched.
	if clipByID(t, p, "a").StartMS != 0 || clipByID(t, p, "b").StartMS != 6000 {
		t.Fatal("input project was mutated")
	}
}

// Loosening the intermediate check must not loosen the committed one.
func TestSingleMoveIntoOverlapStillRejected(t *testing.T) {
	_, _, err := Apply(fixture(t), []Operation{{Type: "move", TargetID: "a", StartMS: int64ptr(3000)}})
	var coded interface{ ErrorCode() string }
	if !errors.As(err, &coded) || coded.ErrorCode() != "OVERLAP" {
		t.Fatalf("expected OVERLAP, got %v", err)
	}
}

// A batch that ends in an illegal state changes nothing, even though an earlier
// step was valid.
func TestBatchEndingInOverlapAppliesNothing(t *testing.T) {
	p := fixture(t)
	_, changes, err := Apply(p, []Operation{
		{Type: "move", TargetID: "b", StartMS: int64ptr(0)},
		{Type: "move", TargetID: "a", StartMS: int64ptr(2000)},
	})
	if err == nil || len(changes) != 0 {
		t.Fatalf("expected rejection without changes, got err=%v changes=%d", err, len(changes))
	}
	var coded interface{ ErrorCode() string }
	if !errors.As(err, &coded) || coded.ErrorCode() != "OVERLAP" {
		t.Fatalf("expected OVERLAP, got %v", err)
	}
	if clipByID(t, p, "a").StartMS != 0 || clipByID(t, p, "b").StartMS != 6000 {
		t.Fatal("rejected batch mutated the project")
	}
}

// The same instruction must be expressible through the offline parser.
func TestOfflineParserCanExpressSwap(t *testing.T) {
	p := fixture(t)
	result, err := OfflineParser{}.Parse(context.Background(), p, []Turn{{"user", "把 b 移到开头，然后把 a 移到 6000 毫秒处"}})
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if result.Status != "plan" || len(result.Operations) != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if _, _, err := Apply(p, result.Operations); err != nil {
		t.Fatalf("parsed plan rejected by engine: %v", err)
	}
}
