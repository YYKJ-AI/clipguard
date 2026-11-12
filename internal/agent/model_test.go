package agent

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

// Detect wrong endpoint/auth, missing project context, and trusting malformed model data.
func TestHTTPModelProtocolAndValidation(t *testing.T){
 cases:=[]struct{name,content string;valid bool}{
 {"plan",`{"status":"plan","message":"准备删除","operations":[{"type":"delete","target_id":"b"}],"candidates":[]}`,true},
 {"clarify",`{"status":"clarify","message":"请说明缩短多少秒","operations":[],"candidates":[]}`,true},
 {"extra field",`{"status":"plan","message":"","operations":[{"type":"delete","target_id":"b","command":"rm"}]}`,false},
 {"unknown op",`{"status":"plan","message":"","operations":[{"type":"execute","target_id":"b"}]}`,false},
 {"code fence","```json\n{}\n```",false},
 {"unknown target",`{"status":"clarify","message":"选择目标","operations":[],"candidates":[{"id":"secret","label":"错误"}]}`,false},
 }
 for _,tc:=range cases{t.Run(tc.name,func(t *testing.T){upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){if r.URL.Path!="/v1/chat/completions"||r.Header.Get("Authorization")!="Bearer test-only"{t.Error("wrong endpoint or auth")};var body map[string]any;if err:=json.NewDecoder(r.Body).Decode(&body);err!=nil{t.Error(err)};if body["model"]!="test-model"{t.Error("model not configured")};raw,_:=json.Marshal(body["messages"]);if !strings.Contains(string(raw),"independent-project"){t.Error("missing context")};_ = json.NewEncoder(w).Encode(map[string]any{"choices":[]any{map[string]any{"finish_reason":"stop","message":map[string]any{"content":tc.content}}}})}));defer upstream.Close();parser:=HTTPModel{BaseURL:upstream.URL+"/v1",Model:"test-model",APIKey:"test-only",Client:upstream.Client()};result,err:=parser.Parse(context.Background(),fixture(t),[]Turn{{Role:"user",Content:"删除 b"}});if tc.valid{if err!=nil{t.Fatal(err)};if result.Status=="plan"{if _,_,err:=Apply(fixture(t),result.Operations);err!=nil{t.Fatal(err)}}}else if err==nil{t.Fatal("invalid model response accepted")}})}
}

func TestModelErrorsDoNotExposeCredentials(t *testing.T){upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.WriteHeader(401);_,_=w.Write([]byte("secret-key-from-upstream"))}));defer upstream.Close();_,err:=(HTTPModel{BaseURL:upstream.URL,Model:"test",Client:upstream.Client()}).Parse(context.Background(),fixture(t),nil);if err==nil||strings.Contains(err.Error(),"secret-key"){t.Fatalf("unsafe error: %v",err)}}
