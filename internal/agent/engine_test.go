package agent

import (
 "encoding/json"
 "reflect"
 "testing"
)

const testProject = `{"project_id":"independent-project","revision":1,"tracks":[{"id":"v1","kind":"video","locked":false},{"id":"v2","kind":"video","locked":false},{"id":"c1","kind":"caption","locked":false},{"id":"locked","kind":"caption","locked":true}],"clips":[{"id":"a","track_id":"v1","start_ms":0,"duration_ms":5000,"source_in_ms":1000,"source_duration_ms":10000},{"id":"b","track_id":"v1","start_ms":6000,"duration_ms":3000,"source_in_ms":0,"source_duration_ms":10000},{"id":"c","track_id":"v2","start_ms":0,"duration_ms":4000,"source_in_ms":0,"source_duration_ms":10000},{"id":"d","track_id":"v2","start_ms":6000,"duration_ms":2000,"source_in_ms":0,"source_duration_ms":10000},{"id":"s","track_id":"c1","start_ms":0,"duration_ms":3000,"text":"欢迎观看"},{"id":"l","track_id":"locked","start_ms":0,"duration_ms":3000,"text":"锁定字幕"}]}`

func fixture(t *testing.T) Project { t.Helper(); p,err:=DecodeProject([]byte(testProject)); if err!=nil {t.Fatal(err)}; return p }
func operations(t *testing.T, raw string) []Operation {t.Helper(); var ops []Operation; if err:=strictJSON([]byte(raw), &ops);err!=nil{t.Fatal(err)};return ops}

// These tests detect wrong time semantics, mutation of the input, and missing operation handling.
func TestEditingOperations(t *testing.T) {
 cases:=[]struct{name,raw,id string;start,duration int64;text string;deleted bool}{
 {"right trim",`[{"type":"trim","target_id":"a","duration_ms":3000}]`,"a",0,3000,"",false},
 {"move",`[{"type":"move","target_id":"b","start_ms":10000,"track_id":"v2"}]`,"b",10000,3000,"",false},
 {"caption",`[{"type":"caption","target_id":"s","start_ms":4000,"duration_ms":2000,"text":"下期见"}]`,"s",4000,2000,"下期见",false},
 {"delete",`[{"type":"delete","target_id":"a"}]`,"a",0,0,"",true},
 }
 for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){p:=fixture(t); before,_:=json.Marshal(p); got,diff,err:=Apply(p,operations(t,tc.raw));if err!=nil{t.Fatal(err)};after,_:=json.Marshal(p);if string(before)!=string(after){t.Fatal("input mutated")};if len(diff)!=1{t.Fatalf("diff: %+v",diff)};found:=false;for _,c:=range got.Clips{if c.ID!=tc.id{continue};found=true;if c.StartMS!=tc.start||c.DurationMS!=tc.duration{t.Fatalf("wrong time %+v",c)};if tc.text!=""&&(c.Text==nil||*c.Text!=tc.text){t.Fatal("wrong caption")};if tc.name=="right trim"&&*c.SourceInMS!=1000{t.Fatal("source in moved")}};if found==tc.deleted{t.Fatal("wrong deletion")}})}
}

func TestInvalidOperationsNeverMutate(t *testing.T){
 cases:=[]string{
 `[{"type":"shell","target_id":"a"}]`,
 `[{"type":"trim","target_id":"missing","duration_ms":1000}]`,
 `[{"type":"trim","target_id":"a","duration_ms":-1}]`,
 `[{"type":"trim","target_id":"a","duration_ms":0}]`,
 `[{"type":"trim","target_id":"a","duration_ms":10000}]`,
 `[{"type":"move","target_id":"b","start_ms":1000}]`,
 `[{"type":"move","target_id":"a","start_ms":9223372036854775807}]`,
 `[{"type":"move","target_id":"a","start_ms":0,"track_id":"c1"}]`,
 `[{"type":"delete","target_id":"l"}]`,
 `[{"type":"caption","target_id":"l","text":"改变"}]`,
 `[{"type":"trim","target_id":"a","duration_ms":3000,"text":"unexpected"}]`,
 `[{"type":"caption","target_id":"a","text":"not caption"}]`,
 `[{"type":"trim","target_id":"a","duration_ms":3000},{"type":"delete","target_id":"l"}]`,
 }
 for _,raw:=range cases{t.Run(raw,func(t *testing.T){p:=fixture(t);before:=Clone(p);var ops []Operation;if err:=strictJSON([]byte(raw),&ops);err!=nil{return};_,_,err:=Apply(p,ops);if err==nil{t.Fatal("invalid operation accepted")};if !reflect.DeepEqual(p,before){t.Fatal("failed batch mutated input")}})}
}

func TestImportRejectsInvalidData(t *testing.T){
 for _,raw:=range []string{`null`,`{"project_id":"x","revision":1,"tracks":[],"clips":[],"extra":true}`,testProject+` {}`,`{"project_id":"x","revision":1,"tracks":[{"id":"v","kind":"video","locked":false}],"clips":[{"id":"a","track_id":"v","start_ms":0,"duration_ms":1}]}`} {if _,err:=DecodeProject([]byte(raw));err==nil{t.Fatalf("accepted invalid project: %s",raw)}}
 p:=fixture(t);p.Clips[1].StartMS=1000;if err:=Validate(p);err==nil{t.Fatal("overlap imported")};p=fixture(t);p.Clips[1].ID="a";if err:=Validate(p);err==nil{t.Fatal("duplicate ID imported")}
}

func TestSequentialBatchAndAdjacentIntervals(t *testing.T){p:=fixture(t);got,diff,err:=Apply(p,operations(t,`[{"type":"trim","target_id":"a","duration_ms":3000},{"type":"move","target_id":"b","start_ms":3000}]`));if err!=nil{t.Fatal(err)};if len(diff)!=2||got.Clips[1].StartMS!=3000{t.Fatal("batch or half-open interval wrong")}}
