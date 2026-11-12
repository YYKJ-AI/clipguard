package agent

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"sync"
	"time"
)

type Binding struct {
	ProjectID string
	Revision  int64
	Epoch     uint64
}
type Preview struct {
	ID         string      `json:"id"`
	ProjectID  string      `json:"project_id"`
	Revision   int64       `json:"revision"`
	Operations []Operation `json:"operations"`
	Changes    []Change    `json:"changes"`
	Summary    string      `json:"summary"`
}
type Result struct {
	Project Project  `json:"project"`
	Changes []Change `json:"changes"`
	Message string   `json:"message"`
}
type HistoryEntry struct {
	ID       string   `json:"id"`
	Action   string   `json:"action"`
	Summary  string   `json:"summary"`
	Revision int64    `json:"revision"`
	Time     string   `json:"time"`
	Changes  []Change `json:"changes"`
}
type History struct {
	Entries     []HistoryEntry `json:"entries"`
	CanUndo     bool           `json:"can_undo"`
	CanRedo     bool           `json:"can_redo"`
	UndoSummary string         `json:"undo_summary"`
	RedoSummary string         `json:"redo_summary"`
}
type pendingPlan struct {
	binding Binding
	preview Preview
}
type historySnapshot struct {
	project Project
	summary string
}
type Workspace struct {
	now     func() time.Time
	mu      sync.Mutex
	project *Project
	epoch   uint64
	plans   map[string]pendingPlan
	history []historySnapshot
	future  []historySnapshot
	records []HistoryEntry
	undos   map[string]bool
	redos   map[string]bool
}

func NewWorkspace() *Workspace {
	return newWorkspace(time.Now)
}

func newWorkspace(now func() time.Time) *Workspace {
	return &Workspace{now: now, plans: map[string]pendingPlan{}, undos: map[string]bool{}, redos: map[string]bool{}}
}
func randomID() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("system random source unavailable")
	}
	return hex.EncodeToString(b[:])
}
func (w *Workspace) resetHistory() {
	w.plans = map[string]pendingPlan{}
	w.history = nil
	w.future = nil
	w.records = nil
	w.undos = map[string]bool{}
	w.redos = map[string]bool{}
}
func (w *Workspace) Load(p Project) error {
	if err := Validate(p); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	v := Clone(p)
	w.project = &v
	w.epoch++
	w.resetHistory()
	return nil
}
func (w *Workspace) binding() Binding {
	if w.project == nil {
		return Binding{Epoch: w.epoch}
	}
	return Binding{w.project.ID, w.project.Revision, w.epoch}
}
func (w *Workspace) Snapshot() (Project, Binding, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.project == nil {
		return Project{}, Binding{}, userError("请先导入工程")
	}
	return Clone(*w.project), w.binding(), nil
}
func (w *Workspace) Preview(b Binding, ops []Operation, summary string) (Preview, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.project == nil || b != w.binding() {
		return Preview{}, userError("工程已变化，请重新提交指令")
	}
	if len(w.plans) >= 64 {
		return Preview{}, userError("待确认计划已达上限，请先取消旧计划")
	}
	ops = cloneOperations(ops)
	_, changes, err := Apply(*w.project, ops)
	if err != nil {
		return Preview{}, err
	}
	if summary == "" {
		summary = fmt.Sprintf("修改 %d 个片段", len(changes))
	}
	preview := Preview{ID: randomID(), ProjectID: b.ProjectID, Revision: b.Revision, Operations: ops, Changes: changes, Summary: summary}
	w.plans[preview.ID] = pendingPlan{b, clonePreview(preview)}
	return preview, nil
}
func (w *Workspace) record(action, summary string, before, after Project) {
	w.records = append(w.records, HistoryEntry{ID: randomID(), Action: action, Summary: summary, Revision: after.Revision, Time: w.now().UTC().Format(time.RFC3339), Changes: Diff(before, after)})
}
func (w *Workspace) checkCapacity() error {
	if w.project.Revision == math.MaxInt64 {
		return userError("版本号已达上限")
	}
	if len(w.records) >= 1000 {
		return userError("编辑记录已达 1000 条，请导出后重新导入")
	}
	return nil
}
func (w *Workspace) Confirm(id string) (Result, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	plan, ok := w.plans[id]
	if !ok {
		return Result{}, userError("计划不存在、已处理或不属于当前会话")
	}
	if plan.binding != w.binding() {
		return Result{}, userError("计划已过期，请重新生成预览")
	}
	if err := w.checkCapacity(); err != nil {
		return Result{}, err
	}
	if len(w.history) >= 100 {
		return Result{}, userError("撤销记录已达 100 条，请导出后重新导入以开始新历史")
	}
	before := Clone(*w.project)
	next, _, err := Apply(*w.project, plan.preview.Operations)
	if err != nil {
		return Result{}, err
	}
	next.Revision = before.Revision + 1
	w.history = append(w.history, historySnapshot{before, plan.preview.Summary})
	w.future = nil
	w.project = &next
	w.plans = map[string]pendingPlan{}
	w.record("edit", plan.preview.Summary, before, next)
	return Result{Clone(next), Diff(before, next), "修改已应用，可整体撤销"}, nil
}
func (w *Workspace) Cancel(id string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.plans[id]; !ok {
		return userError("计划不存在、已处理或不属于当前会话")
	}
	delete(w.plans, id)
	return nil
}
func (w *Workspace) Undo(id string) (Result, error) { return w.restore(id, false) }
func (w *Workspace) Redo(id string) (Result, error) { return w.restore(id, true) }
func (w *Workspace) restore(id string, redo bool) (Result, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	label, action := "撤销", "undo"
	stack := w.history
	requests := w.undos
	if redo {
		label, action = "重做", "redo"
		stack = w.future
		requests = w.redos
	}
	if !validID(id) {
		return Result{}, fmt.Errorf("%s需要有效请求标识", label)
	}
	if requests[id] {
		return Result{}, fmt.Errorf("该%s请求已处理", label)
	}
	if len(requests) >= 1024 {
		return Result{}, fmt.Errorf("%s请求记录已达上限，请导出后重新导入", label)
	}
	if w.project == nil || len(stack) == 0 {
		return Result{}, fmt.Errorf("没有可以%s的修改", label)
	}
	if err := w.checkCapacity(); err != nil {
		return Result{}, err
	}
	last := stack[len(stack)-1]
	before := Clone(*w.project)
	next := Clone(last.project)
	next.Revision = before.Revision + 1
	if err := Validate(next); err != nil {
		return Result{}, err
	}
	if redo {
		w.future = w.future[:len(w.future)-1]
		w.history = append(w.history, historySnapshot{before, last.summary})
	} else {
		w.history = w.history[:len(w.history)-1]
		w.future = append(w.future, historySnapshot{before, last.summary})
	}
	w.project = &next
	requests[id] = true
	w.plans = map[string]pendingPlan{}
	w.record(action, last.summary, before, next)
	return Result{Clone(next), Diff(before, next), "已" + label + "上一条完整指令"}, nil
}
func (w *Workspace) History() History {
	w.mu.Lock()
	defer w.mu.Unlock()
	h := History{Entries: []HistoryEntry{}, CanUndo: len(w.history) > 0, CanRedo: len(w.future) > 0}
	if h.CanUndo {
		h.UndoSummary = w.history[len(w.history)-1].summary
	}
	if h.CanRedo {
		h.RedoSummary = w.future[len(w.future)-1].summary
	}
	if len(w.records) > 0 {
		h.Entries = append([]HistoryEntry(nil), w.records...)
		for i := range h.Entries {
			h.Entries[i].Changes = cloneChanges(w.records[i].Changes)
		}
	}
	return h
}
func int64ptr(n int64) *int64 { return &n }

func cloneOperations(ops []Operation) []Operation {
	if ops == nil {
		return nil
	}
	out := append([]Operation{}, ops...)
	for i := range out {
		o := &out[i]
		o.SourceID = clonePtr(o.SourceID)
		o.StartMS = clonePtr(o.StartMS)
		o.DurationMS = clonePtr(o.DurationMS)
		o.ShortenByMS = clonePtr(o.ShortenByMS)
		o.TrackID = clonePtr(o.TrackID)
		o.Text = clonePtr(o.Text)
	}
	return out
}
func cloneChanges(changes []Change) []Change {
	if changes == nil {
		return nil
	}
	out := append([]Change{}, changes...)
	for i := range out {
		if out[i].Before != nil {
			v := cloneClip(*out[i].Before)
			out[i].Before = &v
		}
		if out[i].After != nil {
			v := cloneClip(*out[i].After)
			out[i].After = &v
		}
	}
	return out
}
func clonePreview(p Preview) Preview {
	p.Operations = cloneOperations(p.Operations)
	p.Changes = cloneChanges(p.Changes)
	return p
}
