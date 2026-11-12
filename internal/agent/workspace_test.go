package agent

import (
 "reflect"
 "sync"
 "sync/atomic"
 "testing"
)

func loaded(t *testing.T)*Workspace{t.Helper();w:=NewWorkspace();if err:=w.Load(fixture(t));err!=nil{t.Fatal(err)};return w}
func previewTrim(t *testing.T,w *Workspace,duration int64)Preview{t.Helper();_,binding,err:=w.Snapshot();if err!=nil{t.Fatal(err)};p,err:=w.Preview(binding,[]Operation{{Type:"trim",TargetID:"a",DurationMS:&duration}});if err!=nil{t.Fatal(err)};return p}

// Detect missing confirmation, stale binding checks, version restoration, and duplicate undo.
func TestConfirmCancelAndUndo(t *testing.T){w:=loaded(t);p:=previewTrim(t,w,3000);before,_,_:=w.Snapshot();if before.Clips[0].DurationMS!=5000{t.Fatal("preview changed project")};if err:=w.Cancel(p.ID);err!=nil{t.Fatal(err)};if _,err:=w.Confirm(p.ID);err==nil{t.Fatal("cancelled plan applied")};p=previewTrim(t,w,3000);result,err:=w.Confirm(p.ID);if err!=nil{t.Fatal(err)};if result.Project.Revision!=2||result.Project.Clips[0].DurationMS!=3000{t.Fatal("wrong commit")};if _,err:=w.Confirm(p.ID);err==nil{t.Fatal("duplicate confirmation accepted")};result,err=w.Undo("undo-1");if err!=nil{t.Fatal(err)};if result.Project.Revision!=3{t.Fatal("undo rolled version back")};restored:=result.Project;restored.Revision=1;if !reflect.DeepEqual(restored,before){t.Fatal("undo did not restore content")};if _,err:=w.Undo("undo-1");err==nil{t.Fatal("duplicate undo accepted")}}
func TestReloadInvalidatesSameProjectAndVersion(t *testing.T){w:=loaded(t);p:=previewTrim(t,w,3000);if err:=w.Load(fixture(t));err!=nil{t.Fatal(err)};if _,err:=w.Confirm(p.ID);err==nil{t.Fatal("reload left plan valid")}}
func TestConcurrentPlansOnlyCommitOnce(t *testing.T){w:=loaded(t);p1:=previewTrim(t,w,3000);p2:=previewTrim(t,w,2000);var successes atomic.Int32;var wg sync.WaitGroup;for _,id:=range []string{p1.ID,p2.ID,p1.ID}{wg.Add(1);go func(id string){defer wg.Done();if _,err:=w.Confirm(id);err==nil{successes.Add(1)}}(id)};wg.Wait();p,_,_:=w.Snapshot();if successes.Load()!=1||p.Revision!=2{t.Fatalf("committed %d times, revision %d",successes.Load(),p.Revision)}}
func TestTwoUndosAndIsolation(t *testing.T){one,two:=loaded(t),loaded(t);p:=previewTrim(t,one,3000);if _,err:=two.Confirm(p.ID);err==nil{t.Fatal("foreign plan accepted")};if _,err:=one.Confirm(p.ID);err!=nil{t.Fatal(err)};p=previewTrim(t,one,2000);if _,err:=one.Confirm(p.ID);err!=nil{t.Fatal(err)};for _,id:=range []string{"u1","u2"}{if _,err:=one.Undo(id);err!=nil{t.Fatal(err)}};got,_,_:=one.Snapshot();other,_,_:=two.Snapshot();if got.Revision!=5||got.Clips[0].DurationMS!=5000||other.Revision!=1||other.Clips[0].DurationMS!=5000{t.Fatal("undo or isolation wrong")}}
func TestParserSnapshotCannotBindToNewState(t *testing.T){w:=loaded(t);_,old,_:=w.Snapshot();p:=previewTrim(t,w,3000);if _,err:=w.Confirm(p.ID);err!=nil{t.Fatal(err)};if _,err:=w.Preview(old,[]Operation{{Type:"trim",TargetID:"a",DurationMS:int64ptr(2000)}});err==nil{t.Fatal("stale parser snapshot accepted")}}
