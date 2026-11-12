package agent

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBearerIdentityIsVerifiedAndStable(t *testing.T) {
	raw := []byte(`{"credentials":[{"subject":"alice","token":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},{"subject":"bob","token":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]}`)
	auth, err := NewBearerAuthenticator(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ header, want string }{
		{"Bearer " + strings.Repeat("a", 32), "alice"}, {"bearer " + strings.Repeat("b", 32), "bob"}, {"Bearer invalid", ""}, {"", ""}, {"Basic " + strings.Repeat("a", 32), ""},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", tc.header)
		r.Header.Set("X-User-ID", "forged")
		got, err := auth.Authenticate(r)
		if tc.want == "" {
			if err == nil {
				t.Fatalf("invalid credentials accepted %q", tc.header)
			}
		} else if err != nil || got != tc.want {
			t.Fatalf("identity: %q %v", got, err)
		}
	}
}

func TestBearerConfigRejectsAmbiguousOrWeakCredentials(t *testing.T) {
	for _, raw := range []string{
		`{}`, `{"credentials":[]}`, `{"credentials":[{"subject":"a","token":"short"}]}`,
		`{"credentials":[{"subject":"a","token":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},{"subject":"b","token":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`,
		`{"credentials":[{"subject":"a","token":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","admin":true}]}`,
	} {
		if _, err := NewBearerAuthenticator([]byte(raw)); err == nil {
			t.Fatalf("invalid config accepted %s", raw)
		}
	}
}

func TestProductBearerRoundTripAndCredentialFailure(t *testing.T) {
	raw := []byte(`{"credentials":[{"subject":"real-user","token":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`)
	auth, err := NewBearerAuthenticator(raw)
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(Config{ProductAuth: auth})
	req := httptest.NewRequest("PUT", "/api/v1/projects/p", strings.NewReader(productProject))
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 32))
	req.Header.Set("If-None-Match", "*")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatal(rec.Body.String())
	}
	req = httptest.NewRequest("GET", "/api/v1/projects/p", nil)
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 32))
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var result struct {
		Project Project `json:"project"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || result.Project.ID != "p" {
		t.Fatal(rec.Body.String())
	}
}
