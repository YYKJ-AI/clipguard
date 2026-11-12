package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// An unconfigured product integration must never inherit anonymous browser sessions.
func TestProductAPIDisabledWithoutAuthentication(t *testing.T) {
	s := NewServer(Config{})
	r := httptest.NewRecorder()
	s.ServeHTTP(r, httptest.NewRequest("GET", "/api/v1/projects/project-one", nil))
	var result struct {
		Error struct {
			Code      string `json:"code"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if r.Code != 503 || result.Error.Code != "INTEGRATION_DISABLED" || result.Error.RequestID == "" {
		t.Fatalf("unconfigured API lacks stable error contract: %d %s", r.Code, r.Body.String())
	}
}

func TestProductModelTimeoutHasStableErrorCode(t *testing.T) {
	parser := waitingParser{make(chan struct{}, 1), make(chan struct{})}
	s := NewServer(Config{ProductAuth: fixtureAuth{}, Model: parser, ModelTimeout: time.Millisecond})
	createProduct(t, s, "alice")
	w := productCall(t, s, "alice", "POST", "/api/v1/projects/p/messages", `{"mode":"model","text":"删除 b"}`, "")
	expectProductCode(t, w, 504, "MODEL_TIMEOUT")
	var out struct {
		Error productError `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if !out.Error.Retryable {
		t.Fatal("timed out preview should be retryable")
	}
}

func TestProductMutationBoundaryErrors(t *testing.T) {
	s := NewServer(Config{ProductAuth: fixtureAuth{}})
	createProduct(t, s, "alice")
	for _, tc := range []struct {
		contentType, origin, body string
		status                    int
		code                      string
	}{
		{"text/plain", "", "{}", 415, "UNSUPPORTED_MEDIA_TYPE"},
		{"application/json", "https://other.invalid", "{}", 403, "FORBIDDEN"},
		{"application/json", "", strings.Repeat(" ", 1<<20) + "{}", 413, "PAYLOAD_TOO_LARGE"},
	} {
		r := httptest.NewRequest("POST", "/api/v1/projects/p/messages", strings.NewReader(tc.body))
		r.Header.Set("Authorization", "Bearer alice-token")
		r.Header.Set("Content-Type", tc.contentType)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		expectProductCode(t, w, tc.status, tc.code)
	}
}

func TestProductCancelAndProjectCapacityPreserveState(t *testing.T) {
	s := NewServer(Config{ProductAuth: fixtureAuth{}, MaxSessions: 1})
	createProduct(t, s, "alice")
	id := productPlan(t, s, "alice", "删除 b")
	w := productCall(t, s, "alice", "POST", "/api/v1/projects/p/plans/"+id+"/cancel", "{}", "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	expectProductCode(t, productCall(t, s, "alice", "POST", "/api/v1/projects/p/plans/"+id+"/confirm", "{}", ""), 409, "STATE_CONFLICT")
	expectProductCode(t, productCall(t, s, "bob", "PUT", "/api/v1/projects/p", productProject, "*"), 503, "PROJECT_CAPACITY_EXCEEDED")
	w = productCall(t, s, "alice", "GET", "/api/v1/projects/p", "", "")
	var state struct {
		Project Project `json:"project"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &state)
	if w.Code != 200 || state.Project.Revision != 1 || len(state.Project.Clips) != 6 {
		t.Fatal("cancel or capacity failure changed existing project")
	}
}

func TestProductModeDisablesAnonymousLegacyAPI(t *testing.T) {
	s := NewServer(Config{ProductAuth: fixtureAuth{}})
	for _, path := range []string{"/api/session", "/api/import", "/api/message", "/api/confirm", "/api/undo"} {
		r := httptest.NewRecorder()
		s.ServeHTTP(r, httptest.NewRequest("POST", path, strings.NewReader("{}")))
		if r.Code != 404 {
			t.Fatalf("product service still exposes anonymous legacy route %s: %d", path, r.Code)
		}
	}
}

var productProject = strings.Replace(testProject, "independent-project", "p", 1)

type fixtureAuth struct{}

func (fixtureAuth) Authenticate(r *http.Request) (string, error) {
	switch r.Header.Get("Authorization") {
	case "Bearer alice-token":
		return "alice", nil
	case "Bearer bob-token":
		return "bob", nil
	}
	return "", errors.New("invalid credential")
}
func productCall(t *testing.T, s http.Handler, owner, method, path, body, condition string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if owner != "" {
		r.Header.Set("Authorization", "Bearer "+owner+"-token")
	}
	r.Header.Set("Content-Type", "application/json")
	if condition == "*" {
		r.Header.Set("If-None-Match", "*")
	} else if condition != "" {
		r.Header.Set("If-Match", condition)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func expectProductCode(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var result struct {
		Error productError `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != status || result.Error.Code != code || result.Error.RequestID == "" || result.Error.RequestID != w.Header().Get("X-Request-ID") {
		t.Fatalf("wrong product error: %d %s", w.Code, w.Body.String())
	}
}
func createProduct(t *testing.T, s http.Handler, owner string) *httptest.ResponseRecorder {
	t.Helper()
	w := productCall(t, s, owner, "PUT", "/api/v1/projects/p", productProject, "*")
	if w.Code != 201 || w.Header().Get("ETag") == "" {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	return w
}
func productPlan(t *testing.T, s http.Handler, owner, text string) string {
	t.Helper()
	data, _ := json.Marshal(map[string]string{"mode": "offline", "text": text})
	w := productCall(t, s, owner, "POST", "/api/v1/projects/p/messages", string(data), "")
	if w.Code != 200 {
		t.Fatalf("plan: %d %s", w.Code, w.Body.String())
	}
	var result struct {
		Preview Preview `json:"preview"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Preview.ID
}

func TestProductVerifiedIdentityAndOwnership(t *testing.T) {
	s := NewServer(Config{ProductAuth: fixtureAuth{}})
	expectProductCode(t, productCall(t, s, "", "GET", "/api/v1/projects/p", "", ""), 401, "UNAUTHENTICATED")
	createProduct(t, s, "alice")
	expectProductCode(t, productCall(t, s, "bob", "GET", "/api/v1/projects/p", "", ""), 404, "PROJECT_NOT_FOUND")
	createProduct(t, s, "bob")
	id := productPlan(t, s, "alice", "把 a 缩短 1 秒")
	expectProductCode(t, productCall(t, s, "bob", "POST", "/api/v1/projects/p/plans/"+id+"/confirm", "{}", ""), 409, "STATE_CONFLICT")
	w := productCall(t, s, "alice", "POST", "/api/v1/projects/p/plans/"+id+"/confirm", "{}", "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var got struct {
		Project Project `json:"project"`
	}
	w = productCall(t, s, "bob", "GET", "/api/v1/projects/p", "", "")
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Project.Revision != 1 || got.Project.Clips[0].DurationMS != 5000 {
		t.Fatal("owner state leaked")
	}
	req := httptest.NewRequest("GET", "/api/v1/projects/p", nil)
	req.Header.Set("X-User-ID", "alice")
	req.AddCookie(&http.Cookie{Name: "safety_session", Value: "not-a-product-identity"})
	out := httptest.NewRecorder()
	s.ServeHTTP(out, req)
	expectProductCode(t, out, 401, "UNAUTHENTICATED")
}

func TestProductConditionalProjectWriteAndReload(t *testing.T) {
	s := NewServer(Config{ProductAuth: fixtureAuth{}})
	created := createProduct(t, s, "alice")
	tag := created.Header().Get("ETag")
	expectProductCode(t, productCall(t, s, "alice", "PUT", "/api/v1/projects/p", productProject, ""), 428, "PRECONDITION_REQUIRED")
	expectProductCode(t, productCall(t, s, "alice", "PUT", "/api/v1/projects/p", productProject, "*"), 412, "PRECONDITION_FAILED")
	expectProductCode(t, productCall(t, s, "alice", "PUT", "/api/v1/projects/p", productProject, `"wrong"`), 412, "PRECONDITION_FAILED")
	id := productPlan(t, s, "alice", "把 a 缩短 1 秒")
	w := productCall(t, s, "alice", "PUT", "/api/v1/projects/p", productProject, tag)
	if w.Code != 200 || w.Header().Get("ETag") == tag {
		t.Fatalf("reload did not change binding: %d %s", w.Code, w.Body.String())
	}
	expectProductCode(t, productCall(t, s, "alice", "POST", "/api/v1/projects/p/plans/"+id+"/confirm", "{}", ""), 409, "STATE_CONFLICT")
	expectProductCode(t, productCall(t, s, "alice", "PUT", "/api/v1/projects/p", productProject, tag), 412, "PRECONDITION_FAILED")
	w = productCall(t, s, "alice", "GET", "/api/v1/projects/p/export", "", "")
	p, err := DecodeProject(w.Body.Bytes())
	if w.Code != 200 || err != nil || p.Revision != 1 {
		t.Fatalf("export: %d %v", w.Code, err)
	}
}

func TestProductConcurrentConditionalReplacement(t *testing.T) {
	s := NewServer(Config{ProductAuth: fixtureAuth{}})
	tag := createProduct(t, s, "alice").Header().Get("ETag")
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- productCall(t, s, "alice", "PUT", "/api/v1/projects/p", productProject, tag).Code
		}()
	}
	wg.Wait()
	close(codes)
	success, conflict := 0, 0
	for code := range codes {
		if code == 200 {
			success++
		}
		if code == 412 {
			conflict++
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("conditional overwrite committed %d times, conflicts %d", success, conflict)
	}
}

func TestProductErrorsAndWholeInstruction(t *testing.T) {
	s := NewServer(Config{ProductAuth: fixtureAuth{}})
	createProduct(t, s, "alice")
	expectProductCode(t, productCall(t, s, "alice", "POST", "/api/v1/projects/p/messages", `{"mode":"offline","text":"删除 b","user_id":"bob"}`, ""), 400, "INVALID_REQUEST")
	expectProductCode(t, productCall(t, s, "alice", "POST", "/api/v1/projects/p/messages", `{"mode":"offline","text":"把 a 缩短 1 秒；删除 l"}`, ""), 422, "EDIT_REJECTED")
	id := productPlan(t, s, "alice", "把 a 缩短 1 秒；把 a 缩短 1 秒")
	path := "/api/v1/projects/p/plans/" + id + "/confirm"
	w := productCall(t, s, "alice", "POST", path, "{}", "")
	var result Result
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if w.Code != 200 || result.Project.Clips[0].DurationMS != 3000 || result.Project.Revision != 2 {
		t.Fatalf("wrong commit: %s", w.Body.String())
	}
	expectProductCode(t, productCall(t, s, "alice", "POST", path, "{}", ""), 409, "STATE_CONFLICT")
	w = productCall(t, s, "alice", "POST", "/api/v1/projects/p/undo", `{"request_id":"u1"}`, "")
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if w.Code != 200 || result.Project.Revision != 3 || result.Project.Clips[0].DurationMS != 5000 {
		t.Fatal(w.Body.String())
	}
	expectProductCode(t, productCall(t, s, "alice", "POST", "/api/v1/projects/p/undo", `{"request_id":"u1"}`, ""), 409, "STATE_CONFLICT")
	expectProductCode(t, productCall(t, s, "alice", "DELETE", "/api/v1/projects/p", "", ""), 405, "METHOD_NOT_ALLOWED")
	expectProductCode(t, productCall(t, s, "alice", "PUT", "/api/v1/projects/wrong", productProject, "*"), 422, "PROJECT_ID_MISMATCH")
	expectProductCode(t, productCall(t, s, "alice", "PUT", "/api/v1/projects/p", strings.Repeat(" ", 1<<20)+productProject, `"any"`), 413, "PAYLOAD_TOO_LARGE")
}

type envelopeCodec struct{}

func (envelopeCodec) Name() string { return "fixture.v1" }
func (envelopeCodec) Decode(raw []byte) (Project, error) {
	var v struct {
		Timeline Project `json:"timeline"`
	}
	if err := strictJSON(raw, &v); err != nil {
		return Project{}, err
	}
	return v.Timeline, Validate(v.Timeline)
}
func (envelopeCodec) Encode(p Project) ([]byte, error) {
	return json.Marshal(map[string]Project{"timeline": p})
}
func TestProductFormatAdapter(t *testing.T) {
	s := NewServer(Config{ProductAuth: fixtureAuth{}, ProjectCodec: envelopeCodec{}})
	w := productCall(t, s, "alice", "PUT", "/api/v1/projects/p", `{"timeline":`+productProject+`}`, "*")
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	w = productCall(t, s, "alice", "GET", "/api/v1/projects/p/export", "", "")
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"timeline"`)) {
		t.Fatal(w.Body.String())
	}
	if _, err := (envelopeCodec{}).Decode(w.Body.Bytes()); err != nil {
		t.Fatal(err)
	}
}
