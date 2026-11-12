package agent

import (
 "bytes"
 "context"
 "encoding/json"
 "fmt"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"
)

func call(t *testing.T,s http.Handler,cookie *http.Cookie,method,path,body string)(*httptest.ResponseRecorder,map[string]any){t.Helper();req:=httptest.NewRequest(method,path,bytes.NewBufferString(body));if cookie!=nil{req.AddCookie(cookie)};req.Header.Set("Content-Type","application/json");rec:=httptest.NewRecorder();s.ServeHTTP(rec,req);var data map[string]any;if err:=json.Unmarshal(rec.Body.Bytes(),&data);err!=nil{t.Fatalf("non JSON: %s",rec.Body.String())};return rec,data}
func session(t *testing.T,s http.Handler)*http.Cookie{t.Helper();r,_:=call(t,s,nil,"POST","/api/session",`{}`);if r.Code!=200{t.Fatal(r.Body.String())};return r.Result().Cookies()[0]}

func TestHTTPWorkflowAndForeignPlan(t *testing.T){s:=NewServer(Config{});one,two:=session(t,s),session(t,s);for _,c:=range []*http.Cookie{one,two}{r,_:=call(t,s,c,"POST","/api/import",testProject);if r.Code!=200{t.Fatal(r.Body.String())}};r,data:=call(t,s,one,"POST","/api/message",`{"text":"把 a 缩短 2 秒","mode":"offline"}`);if r.Code!=200{t.Fatal(r.Body.String())};id:=data["preview"].(map[string]any)["id"].(string);r,_=call(t,s,two,"POST","/api/confirm",fmt.Sprintf(`{"plan_id":%q}`,id));if r.Code==200{t.Fatal("foreign plan accepted")};r,_=call(t,s,one,"POST","/api/confirm",fmt.Sprintf(`{"plan_id":%q}`,id));if r.Code!=200{t.Fatal(r.Body.String())};r,data=call(t,s,one,"GET","/api/export","");if r.Code!=200||data["revision"]!=float64(2){t.Fatal("wrong export")};_,data=call(t,s,two,"GET","/api/export","");if data["revision"]!=float64(1){t.Fatal("session leaked")};r,_=call(t,s,one,"POST","/api/undo",`{"request_id":"u-1"}`);if r.Code!=200{t.Fatal(r.Body.String())}}

func TestHTTPClarification(t *testing.T){s:=NewServer(Config{});c:=session(t,s);call(t,s,c,"POST","/api/import",testProject);r,data:=call(t,s,c,"POST","/api/message",`{"text":"把第二段移到开头","mode":"offline"}`);if r.Code!=200||data["status"]!="clarify"{t.Fatal(r.Body.String())};id:=data["clarification_id"].(string);r,_=call(t,s,c,"POST","/api/message",fmt.Sprintf(`{"choice":"b","clarification_id":%q,"mode":"offline"}`,id));if r.Code==200{t.Fatal("collision after choice must fail validation")};_,data=call(t,s,c,"GET","/api/export","");if data["revision"]!=float64(1){t.Fatal("clarification modified project")}}


type waitingParser struct{started chan struct{};release chan struct{}}
func(p waitingParser)Parse(ctx context.Context,project Project,turns []Turn)(ParseResult,error){select{case p.started<-struct{}{}:default:};select{case <-p.release:return ParseResult{Status:"plan",Operations:[]Operation{{Type:"delete",TargetID:"b"}}},nil;case <-ctx.Done():return ParseResult{},ctx.Err()}}
func TestModelCapacityAndStaleResponse(t *testing.T){parser:=waitingParser{make(chan struct{},1),make(chan struct{})};s:=NewServer(Config{Model:parser,ModelConcurrency:1});one,two:=session(t,s),session(t,s);for _,c:=range []*http.Cookie{one,two}{call(t,s,c,"POST","/api/import",testProject)};done:=make(chan int,1);go func(){r,_:=call(t,s,one,"POST","/api/message",`{"text":"删除第二段","mode":"model"}`);done<-r.Code}();select{case <-parser.started:case <-time.After(3*time.Second):t.Fatal("model not called")};r,_:=call(t,s,two,"POST","/api/message",`{"text":"删除第二段","mode":"model"}`);if r.Code!=503{t.Fatalf("unbounded model request: %d",r.Code)};call(t,s,one,"POST","/api/import",testProject);close(parser.release);select{case code:=<-done:if code==200{t.Fatal("stale model response accepted")};case <-time.After(3*time.Second):t.Fatal("model request stuck")}}

func TestHTTPRejectsCrossOriginAndUnknownFields(t *testing.T){s:=NewServer(Config{});req:=httptest.NewRequest("POST","http://localhost/api/session",bytes.NewBufferString(`{}`));req.Header.Set("Origin","https://evil.invalid");r:=httptest.NewRecorder();s.ServeHTTP(r,req);if r.Code!=403{t.Fatal("cross origin write accepted")};r,_=call(t,s,nil,"GET","/api/state","");if r.Code!=401{t.Fatal("anonymous state exposed")};c:=session(t,s);r,_=call(t,s,c,"POST","/api/import",testProject[:len(testProject)-1]+`,"extra":1}`);if r.Code==200{t.Fatal("unknown import fields accepted")}}

// TestOfflineIsDefaultAndAcceptancePath proves the acceptance path required by
// the task book needs no model, no key and no network: with Config{Model:nil},
// edit, clarification, rejection, cancel and undo all run through OfflineParser.
func TestOfflineIsDefaultAndAcceptancePath(t *testing.T) {
	s := NewServer(Config{})
	r, data := call(t, s, nil, "POST", "/api/session", `{}`)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if available, _ := data["model_available"].(bool); available {
		t.Fatal("model reported available without configuration")
	}
	c := session(t, s)
	if r, _ := call(t, s, c, "POST", "/api/import", testProject); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	clipDuration := func(id string) float64 {
		t.Helper()
		_, exported := call(t, s, c, "GET", "/api/export", "")
		for _, raw := range exported["clips"].([]any) {
			clip := raw.(map[string]any)
			if clip["id"] == id {
				return clip["duration_ms"].(float64)
			}
		}
		t.Fatalf("clip %s missing", id)
		return 0
	}

	// An omitted mode must not require an explicit choice: offline is the default.
	r, data = call(t, s, c, "POST", "/api/message", `{"text":"把 a 缩短 2 秒"}`)
	if r.Code != 200 || data["status"] != "plan" {
		t.Fatalf("default mode is not offline: %s", r.Body.String())
	}
	planID := data["preview"].(map[string]any)["id"].(string)
	if r, _ = call(t, s, c, "POST", "/api/confirm", fmt.Sprintf(`{"plan_id":%q}`, planID)); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if got := clipDuration("a"); got != 3000 {
		t.Fatalf("edit not applied: %v", got)
	}
	if r, _ = call(t, s, c, "POST", "/api/undo", `{"request_id":"acceptance-undo-1"}`); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if got := clipDuration("a"); got != 5000 {
		t.Fatalf("undo did not restore content: %v", got)
	}

	// Ambiguity, locked-track rejection and cancelled delete all work offline.
	r, data = call(t, s, c, "POST", "/api/message", `{"text":"把第二段移到开头","mode":"offline"}`)
	if r.Code != 200 || data["status"] != "clarify" {
		t.Fatalf("clarification unavailable offline: %s", r.Body.String())
	}
	r, _ = call(t, s, c, "POST", "/api/message", `{"text":"把 l 的字幕改为“改动”","mode":"offline"}`)
	if r.Code == 200 || !strings.Contains(r.Body.String(), "锁定") {
		t.Fatalf("locked track not rejected offline: %s", r.Body.String())
	}
	r, data = call(t, s, c, "POST", "/api/message", `{"text":"删除 b","mode":"offline"}`)
	if r.Code != 200 || data["status"] != "plan" {
		t.Fatalf("delete preview unavailable offline: %s", r.Body.String())
	}
	cancelID := data["preview"].(map[string]any)["id"].(string)
	if r, _ = call(t, s, c, "POST", "/api/cancel", fmt.Sprintf(`{"plan_id":%q}`, cancelID)); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	_, exported := call(t, s, c, "GET", "/api/export", "")
	if exported["revision"] != float64(3) {
		t.Fatalf("cancel or rejection changed the project: %v", exported["revision"])
	}

	// Model mode is opt-in and must fail closed when no model is configured.
	if r, _ = call(t, s, c, "POST", "/api/message", `{"text":"把 a 缩短 1 秒","mode":"model"}`); r.Code != 503 {
		t.Fatalf("model mode without configuration returned %d", r.Code)
	}
}
