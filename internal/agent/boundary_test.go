package agent

import (
	"errors"
	"net/http/httptest"
	"testing"
)

// A typo can silently override a field when duplicate keys are accepted; the
// last value would win without anyone noticing.
func TestDuplicateJSONKeysAreRejected(t *testing.T) {
	const project = `{"project_id":"dup","revision":1,"revision":2,"tracks":[{"id":"v","kind":"video","locked":false}],"clips":[]}`
	if _, err := DecodeProject([]byte(project)); err == nil {
		t.Fatal("duplicate project field was accepted")
	} else {
		var coded interface{ ErrorCode() string }
		if !errors.As(err, &coded) || coded.ErrorCode() != "INVALID_JSON" {
			t.Fatalf("expected INVALID_JSON, got %v", err)
		}
	}
	var ops []Operation
	duplicateOp := `[{"type":"trim","target_id":"a","shorten_by_ms":1000,"shorten_by_ms":2000}]`
	if err := strictJSON([]byte(duplicateOp), &ops); err == nil {
		t.Fatal("duplicate operation field was accepted")
	}
	// Nested objects are checked too, not only the root object.
	nested := `{"project_id":"x","revision":1,"tracks":[{"id":"v","id":"v2","kind":"video","locked":false}],"clips":[]}`
	if _, err := DecodeProject([]byte(nested)); err == nil {
		t.Fatal("duplicate nested field was accepted")
	}
}

// DNS rebinding keeps Origin and Host equal, so only an explicit Host allowlist
// stops it.
func TestForeignHostIsRejected(t *testing.T) {
	server := NewServer(Config{AllowedHosts: []string{"127.0.0.1", "localhost", "::1"}})
	request := httptest.NewRequest("GET", "/api/state", nil)
	request.Host = "attacker.example:8090"
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != 403 {
		t.Fatalf("foreign host accepted: %d", recorder.Code)
	}
}

func TestLoopbackHostsAreAccepted(t *testing.T) {
	server := NewServer(Config{AllowedHosts: []string{"127.0.0.1", "localhost", "::1"}})
	for _, host := range []string{"127.0.0.1:8090", "localhost:8090", "[::1]:8090", "LOCALHOST:8090"} {
		request := httptest.NewRequest("GET", "/api/state", nil)
		request.Host = host
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code == 403 {
			t.Fatalf("loopback host %s rejected", host)
		}
	}
}
