package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Model            Parser
	ModelConcurrency int
	MaxSessions      int
	Sample           []byte
	ModelTimeout     time.Duration
	ProductAuth      Authenticator
	ProjectCodec     ProjectCodec
}
type clarification struct {
	ID       string
	Binding  Binding
	Result   ParseResult
	Original string
	Mode     string
	Choices  map[int]string
}

var quantityAnswerPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]{1,3})?\s*(?:秒|毫秒)$`)

type userSession struct {
	workspace          *Workspace
	dialogMu           sync.Mutex
	turns              []Turn
	dialogBinding      Binding
	pending            *clarification
	lastCaptionID      string
	lastCaptionBinding Binding
}

func (u *userSession) captureRecentCaption(result Result) {
	u.dialogMu.Lock()
	defer u.dialogMu.Unlock()
	for _, change := range result.Changes {
		if change.After != nil && change.After.Text != nil {
			u.lastCaptionID = change.After.ID
			_, u.lastCaptionBinding, _ = u.workspace.Snapshot()
		} else if change.ID == u.lastCaptionID && change.After == nil {
			u.lastCaptionID = ""
		}
	}
}

func (u *userSession) clearRecentCaption() {
	u.dialogMu.Lock()
	u.lastCaptionID = ""
	u.dialogMu.Unlock()
}

type Server struct {
	config            Config
	mu                sync.Mutex
	sessions          map[string]*userSession
	slots             chan struct{}
	productProjects   map[projectKey]*userSession
	productGeneration string
	modelStatus       ModelStatus
	modelCheckMu      sync.Mutex
}

func NewServer(config Config) *Server {
	if config.MaxSessions <= 0 {
		config.MaxSessions = 128
	}
	if config.ModelConcurrency <= 0 {
		config.ModelConcurrency = 4
	}
	if config.ModelTimeout <= 0 {
		config.ModelTimeout = 45 * time.Second
	}
	return &Server{config: config, sessions: map[string]*userSession{}, slots: make(chan struct{}, config.ModelConcurrency), productGeneration: randomID()}
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, err error) {
	respond(w, status, map[string]string{"error": publicError(err.Error())})
}
func body(r *http.Request, dest any) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 1<<20 {
		return errors.New("请求超过 1 MiB 限制")
	}
	return strictJSON(data, dest)
}
func (s *Server) find(r *http.Request) *userSession {
	cookie, err := r.Cookie("safety_session")
	if err != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[cookie.Value]
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/v1/") {
		s.productHTTP(w, r)
		return
	}
	if s.config.ProductAuth != nil {
		fail(w, 404, errors.New("产品服务模式仅开放 /api/v1 接口"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != "GET" && r.Method != "POST" {
		fail(w, 405, errors.New("不支持的请求方法"))
		return
	}
	if r.Method == "POST" {
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			fail(w, 403, errors.New("拒绝跨站修改请求"))
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			if err != nil || u.Host != r.Host || u.Scheme != scheme {
				fail(w, 403, errors.New("拒绝跨站修改请求"))
				return
			}
		}
	}
	if r.URL.Path == "/api/session" && r.Method == "POST" {
		u := s.find(r)
		if u == nil {
			s.mu.Lock()
			if len(s.sessions) >= s.config.MaxSessions {
				s.mu.Unlock()
				fail(w, 503, errors.New("会话容量已满，请联系运行者；已有工程不会被清理"))
				return
			}
			id := randomID()
			u = &userSession{workspace: NewWorkspace()}
			s.sessions[id] = u
			s.mu.Unlock()
			http.SetCookie(w, &http.Cookie{Name: "safety_session", Value: id, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode})
		}
		respond(w, 200, map[string]any{"model_available": s.config.Model != nil, "max_history": 100})
		return
	}
	if r.URL.Path == "/api/sample" && r.Method == "GET" {
		if len(s.config.Sample) == 0 {
			fail(w, 404, errors.New("无样例"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(s.config.Sample)
		return
	}
	u := s.find(r)
	if u == nil {
		fail(w, 401, errors.New("会话不存在，请刷新页面重新连接"))
		return
	}
	switch {
	case r.URL.Path == "/api/model/status" && r.Method == "GET":
		respond(w, 200, s.ModelStatus())
	case r.URL.Path == "/api/model/check" && r.Method == "POST":
		var input struct{}
		if err := body(r, &input); err != nil {
			fail(w, 400, err)
			return
		}
		s.checkModel(w, r)
	case r.URL.Path == "/api/history" && r.Method == "GET":
		respond(w, 200, u.workspace.History())
	case r.URL.Path == "/api/insert-preview" && r.Method == "POST":
		var input struct {
			SourceID string `json:"source_id"`
			TrackID  string `json:"track_id"`
			StartMS  *int64 `json:"start_ms"`
		}
		if err := body(r, &input); err != nil {
			fail(w, 400, err)
			return
		}
		if input.StartMS == nil {
			fail(w, 400, errors.New("请明确提供插入起点"))
			return
		}
		_, binding, err := u.workspace.Snapshot()
		if err != nil {
			fail(w, 400, err)
			return
		}
		op := Operation{Type: "insert", TargetID: "insert-" + randomID(), SourceID: &input.SourceID, TrackID: &input.TrackID, StartMS: input.StartMS}
		preview, err := u.workspace.Preview(binding, []Operation{op}, "插入已有片段的副本")
		if err != nil {
			fail(w, 422, err)
			return
		}
		respond(w, 200, map[string]any{"status": "plan", "preview": preview, "message": "请检查新副本的位置，确认后才会插入"})
	case r.URL.Path == "/api/state" && r.Method == "GET":
		p, _, err := u.workspace.Snapshot()
		if err != nil {
			respond(w, 200, map[string]any{"project": nil})
			return
		}
		respond(w, 200, map[string]any{"project": p})
	case r.URL.Path == "/api/export" && r.Method == "GET":
		p, _, err := u.workspace.Snapshot()
		if err != nil {
			fail(w, 400, err)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="timeline.json"`)
		respond(w, 200, p)
	case r.URL.Path == "/api/import" && r.Method == "POST":
		var p Project
		if err := body(r, &p); err != nil {
			fail(w, 400, err)
			return
		}
		if err := u.workspace.Load(p); err != nil {
			fail(w, 400, err)
			return
		}
		respond(w, 200, map[string]any{"project": p, "message": "工程已导入，旧计划和撤销历史已清空"})
	case r.URL.Path == "/api/message" && r.Method == "POST":
		s.message(w, r, u)
	case r.URL.Path == "/api/message/stream" && r.Method == "POST":
		s.streamMessage(w, r, u)
	case r.URL.Path == "/api/confirm" && r.Method == "POST":
		var v struct {
			PlanID string `json:"plan_id"`
		}
		if err := body(r, &v); err != nil {
			fail(w, 400, err)
			return
		}
		result, err := u.workspace.Confirm(v.PlanID)
		if err != nil {
			fail(w, 409, err)
			return
		}
		u.captureRecentCaption(result)
		respond(w, 200, result)
	case r.URL.Path == "/api/cancel" && r.Method == "POST":
		var v struct {
			PlanID string `json:"plan_id"`
		}
		if err := body(r, &v); err != nil {
			fail(w, 400, err)
			return
		}
		if err := u.workspace.Cancel(v.PlanID); err != nil {
			fail(w, 409, err)
			return
		}
		respond(w, 200, map[string]string{"message": "已取消，工程没有变化"})
	case (r.URL.Path == "/api/undo" || r.URL.Path == "/api/redo") && r.Method == "POST":
		var v struct {
			RequestID string `json:"request_id"`
		}
		if err := body(r, &v); err != nil {
			fail(w, 400, err)
			return
		}
		var result Result
		var err error
		if r.URL.Path == "/api/redo" {
			result, err = u.workspace.Redo(v.RequestID)
		} else {
			result, err = u.workspace.Undo(v.RequestID)
		}
		if err != nil {
			fail(w, 409, err)
			return
		}
		u.clearRecentCaption()
		respond(w, 200, result)
	default:
		fail(w, 404, errors.New("接口不存在或请求方法不匹配"))
	}
}

func validateParseResult(p Project, result ParseResult) error {
	if len(result.Message) > 6000 {
		return errors.New("解析说明过长")
	}
	switch result.Status {
	case "answer":
		if len(result.Operations) > 0 || len(result.Candidates) > 0 || strings.TrimSpace(result.Message) == "" {
			return errors.New("咨询回复必须有正文，且不能包含操作或候选")
		}
	case "plan":
		if len(result.Operations) == 0 || len(result.Operations) > 64 || len(result.Candidates) > 0 {
			return errors.New("操作计划结构无效")
		}
	case "clarify":
		if len(result.Operations) > 0 || strings.TrimSpace(result.Message) == "" || len(result.Candidates) > 64 {
			return errors.New("澄清结构无效")
		}
		seen := map[string]bool{}
		for _, candidate := range result.Candidates {
			found := false
			for _, c := range p.Clips {
				if c.ID == candidate.ID {
					found = true
					break
				}
			}
			if !found || seen[candidate.ID] || len(candidate.Label) > 512 {
				return errors.New("澄清包含未知或重复目标")
			}
			seen[candidate.ID] = true
		}
	case "unsupported":
		if len(result.Operations) > 0 || len(result.Candidates) > 0 {
			return errors.New("不支持响应不能包含操作或候选")
		}
	default:
		return errors.New("未知解析状态")
	}
	return nil
}

func (s *Server) message(w http.ResponseWriter, r *http.Request, u *userSession) {
	var input struct {
		Text            string `json:"text"`
		Mode            string `json:"mode"`
		Choice          string `json:"choice"`
		ClarificationID string `json:"clarification_id"`
		SelectedClipID  string `json:"selected_clip_id"`
	}
	if err := body(r, &input); err != nil {
		fail(w, 400, err)
		return
	}
	if input.Mode == "" {
		input.Mode = "offline"
	}
	if input.Mode != "offline" && input.Mode != "model" {
		fail(w, 400, errors.New("请选择 offline 或 model 模式"))
		return
	}
	if len(input.Text) > 4000 {
		fail(w, 400, errors.New("指令超过长度限制"))
		return
	}
	if !u.dialogMu.TryLock() {
		fail(w, 409, errors.New("本会话正在处理上一条指令，请稍后重试"))
		return
	}
	defer u.dialogMu.Unlock()
	p, binding, err := u.workspace.Snapshot()
	hasProject := err == nil
	if input.SelectedClipID != "" {
		found := false
		for _, clip := range p.Clips {
			if clip.ID == input.SelectedClipID {
				found = true
				break
			}
		}
		if !hasProject || !found {
			fail(w, 400, errors.New("所选片段不在当前工程中，请重新选择"))
			return
		}
	}
	if err != nil && input.Mode != "model" {
		fail(w, 400, err)
		return
	}
	if binding != u.dialogBinding {
		u.turns = nil
		u.pending = nil
		u.dialogBinding = binding
	}
	if u.lastCaptionBinding.ProjectID != binding.ProjectID || u.lastCaptionBinding.Epoch != binding.Epoch {
		u.lastCaptionID = ""
	}
	text := strings.TrimSpace(input.Text)
	if input.Choice == "" && text == "取消" {
		u.turns, u.pending = nil, nil
		respond(w, 200, map[string]string{"status": "answer", "message": "已取消当前澄清，本次没有修改工程。若页面仍有待确认预览，请在预览卡片上点取消。"})
		return
	}
	if input.Choice == "" && u.pending != nil && u.pending.Mode == input.Mode && u.pending.Binding == binding {
		if strings.HasPrefix(text, "改成") {
			text = strings.TrimSpace(strings.TrimPrefix(text, "改成"))
			u.turns, u.pending = nil, nil
		} else if strings.HasPrefix(text, "算了，改成") {
			text = strings.TrimSpace(strings.TrimPrefix(text, "算了，改成"))
			u.turns, u.pending = nil, nil
		} else if input.Mode == "offline" && quantityAnswerPattern.MatchString(text) && strings.HasSuffix(strings.TrimSpace(u.pending.Original), "缩短") {
			text = u.pending.Original + " " + text
			u.pending = nil
		} else {
			u.turns, u.pending = nil, nil
		}
	}
	choices := map[int]string{}
	if input.Choice != "" {
		pending := u.pending
		if pending == nil || pending.ID != input.ClarificationID || pending.Binding != binding || pending.Mode != input.Mode {
			fail(w, 409, errors.New("澄清已失效，请重新输入指令"))
			return
		}
		valid := false
		for _, c := range pending.Result.Candidates {
			if c.ID == input.Choice {
				valid = true
			}
		}
		if !valid {
			fail(w, 400, errors.New("选择不属于候选列表"))
			return
		}
		if input.Mode == "offline" {
			text = pending.Original
			for index, target := range pending.Choices {
				choices[index] = target
			}
			choices[pending.Result.CommandIndex] = input.Choice
		} else {
			text = "我选择的目标 ID 是 " + input.Choice + "。请结合上一条修改要求生成完整候选计划，不要执行。"
		}
	}
	if text == "" {
		fail(w, 400, errors.New("请输入指令或补充信息"))
		return
	}
	turns := append([]Turn(nil), u.turns...)
	if input.Mode == "offline" {
		turns = nil
	}
	turns = append(turns, Turn{"user", text})
	if len(turns) > 9 {
		fail(w, 400, errors.New("本轮澄清过长，请重新导入工程后给出完整指令"))
		return
	}
	var parser Parser = OfflineParser{choices: choices, selectedID: input.SelectedClipID, lastCaptionID: u.lastCaptionID}
	if input.Mode == "model" {
		if s.config.Model == nil {
			fail(w, 503, errors.New("模型尚未配置，请使用离线句式模式"))
			return
		}
		parser = s.config.Model
		select {
		case s.slots <- struct{}{}:
			defer func() { <-s.slots }()
		default:
			fail(w, 503, errors.New("模型服务繁忙，请稍后重试"))
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.config.ModelTimeout)
	defer cancel()
	ctx = context.WithValue(ctx, modelSelectionKey{}, modelSelection{SelectedID: input.SelectedClipID, LastCaptionID: u.lastCaptionID})
	if sink, ok := ctx.Value(streamSinkKey{}).(streamSink); ok {
		sink("progress", map[string]string{"message": "正在理解你的要求…"})
		if mediaCapabilityGap(text) || exportCapabilityQuestion(text) || generalCapabilityQuestion(text) {
			// Capability feedback is normalized below. Do not expose an earlier,
			// contradictory provisional claim from the model.
			ctx = context.WithValue(ctx, streamSinkKey{}, streamSink(func(kind string, data any) {
				if kind != "draft" {
					sink(kind, data)
				}
			}))
		}
	}
	result, err := parser.Parse(ctx, p, turns)
	if ctx.Err() != nil {
		u.turns, u.pending = nil, nil
		fail(w, 504, errors.New(instructionFeedback(p, "模型或解析请求超时")))
		return
	}
	if err != nil {
		u.turns, u.pending = nil, nil
		if input.Mode == "model" {
			fail(w, 422, errors.New(err.Error()+"。本次没有修改工程。"))
		} else {
			fail(w, 422, errors.New(instructionFeedback(p, err.Error())))
		}
		return
	}
	_, current, currentErr := u.workspace.Snapshot()
	if (hasProject && (currentErr != nil || current != binding)) || (!hasProject && currentErr == nil) {
		fail(w, 409, errors.New("处理期间工程已变化，请重新提交指令"))
		return
	}
	if mediaCapabilityGap(text) {
		result = ParseResult{Status: "unsupported", Message: "暂时不支持自动检测或修复异常画面，也不能在去除画面时单独保留对应旁白；当前工程只支持片段移动、视频右端缩短、字幕调整和删除。工程没有变化。"}
	} else if input.Mode == "model" {
		if result.Status == "answer" && exportCapabilityQuestion(text) {
			result.Message = "可以导出当前工程 JSON，用于保存时间轴修改；当前不支持把工程编码导出为视频成片。"
		} else if result.Status == "answer" && generalCapabilityQuestion(text) {
			result.Message = "我可以移动已有片段、从视频右端缩短、修改字幕、删除，以及插入已有片段的副本。修改前会展示差异，确认后才执行；可以整体撤销和重做，查看编辑记录，绑定本地视频或图片预览。可以保存工程 JSON，不能导出视频成片；画面检测修复、降噪、自动字幕和音画联动暂时不支持。"
		}
	}
	if err := validateParseResult(p, result); err != nil {
		u.turns, u.pending = nil, nil
		fail(w, 422, errors.New(instructionFeedback(p, err.Error())))
		return
	}
	switch result.Status {
	case "answer":
		u.pending = nil
		u.turns = append(turns, Turn{"assistant", result.Message})
		if len(u.turns) > 8 {
			u.turns = append([]Turn(nil), u.turns[len(u.turns)-8:]...)
		}
		respond(w, 200, map[string]string{"status": "answer", "message": result.Message})
	case "plan":
		if !hasProject {
			u.turns, u.pending = nil, nil
			respond(w, 200, map[string]any{"status": "clarify", "message": "请先加载演示工程或导入 JSON，再告诉我需要怎样修改。你可以点击左侧的“加载演示工程”。本次没有修改工程。", "candidates": []Candidate{}, "clarification_id": randomID()})
			return
		}
		preview, err := u.workspace.Preview(binding, result.Operations, text)
		if err != nil {
			u.turns, u.pending = nil, nil
			fail(w, 422, errors.New(instructionFeedback(p, err.Error())))
			return
		}
		u.turns = nil
		u.pending = nil
		respond(w, 200, map[string]any{"status": "plan", "preview": preview, "message": "请检查实际变更，确认后才会修改工程"})
	case "clarify":
		id := randomID()
		u.pending = &clarification{ID: id, Binding: binding, Result: result, Original: text, Mode: input.Mode, Choices: choices}
		u.turns = append(turns, Turn{"assistant", result.Message + fmt.Sprint(result.Candidates)})
		respond(w, 200, map[string]any{"status": "clarify", "message": result.Message, "candidates": result.Candidates, "clarification_id": id})
	default:
		u.turns = nil
		u.pending = nil
		if input.Mode == "model" || mediaCapabilityGap(text) {
			respond(w, 200, map[string]string{"status": "unsupported", "message": result.Message})
		} else {
			respond(w, 200, map[string]string{"status": "unsupported", "message": instructionFeedback(p, result.Message)})
		}
	}
}

func mediaCapabilityGap(text string) bool {
	// Quoted caption content is data. Negated clauses are not capability requests.
	var plain strings.Builder
	var quote rune
	escaped := false
	for _, r := range text {
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if quote == '"' && r == '\\' {
				escaped = true
				continue
			}
			if (quote == '“' && r == '”') || (quote == '"' && r == '"') {
				quote = 0
			}
			continue
		}
		if r == '“' || r == '"' {
			quote = r
			continue
		}
		plain.WriteRune(r)
	}
	for _, clause := range strings.FieldsFunc(plain.String(), func(r rune) bool { return strings.ContainsRune("，,；;。\n", r) }) {
		if mediaCapabilityClause(clause) {
			return true
		}
	}
	return false
}

func mediaCapabilityClause(text string) bool {
	visual := strings.Contains(text, "画面") || strings.Contains(text, "花屏") || strings.Contains(text, "噪点") || strings.Contains(text, "黑屏") || strings.Contains(text, "蓝屏")
	action := mediaActionPattern.MatchString(strings.TrimSpace(text))
	problem := strings.Contains(text, "异常") || strings.Contains(text, "纯色") || strings.Contains(text, "花屏") || strings.Contains(text, "噪点") || strings.Contains(text, "黑屏") || strings.Contains(text, "蓝屏")
	return visual && action && problem
}

var mediaActionPattern = regexp.MustCompile(`^(?:请)?(?:帮我|麻烦)?(?:自动)?(?:检查|检测|修复|处理|去掉)`)

func generalCapabilityQuestion(text string) bool {
	return strings.Contains(text, "有哪些功能") || strings.Contains(text, "能做什么") || strings.Contains(text, "支持哪些功能")
}

func exportCapabilityQuestion(text string) bool {
	return strings.Contains(text, "导出") && (strings.Contains(text, "吗") || strings.Contains(text, "能") || strings.Contains(text, "支持") || strings.Contains(text, "功能"))
}

func instructionFeedback(p Project, reason string) string {
	return publicError(reason) + "。本条指令未执行任何修改。请核对选中的片段与修改量，参考页面示例重新提交完整指令。离线模式用数字加秒或毫秒；组合操作用分号分隔，字幕正文使用双引号。仍需查看预览并确认。"
}
