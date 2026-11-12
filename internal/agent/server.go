package agent

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Logger           *slog.Logger
	SessionIdleTTL   time.Duration
	Now              func() time.Time
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
	lastSeen           time.Time
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
	creationWindows   map[string]creationWindow
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
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.SessionIdleTTL <= 0 {
		config.SessionIdleTTL = 30 * time.Minute
	}
	if config.MaxSessions <= 0 {
		config.MaxSessions = 128
	}
	if config.ModelConcurrency <= 0 {
		config.ModelConcurrency = 4
	}
	if config.ModelTimeout <= 0 {
		config.ModelTimeout = 45 * time.Second
	}
	return &Server{creationWindows: map[string]creationWindow{}, config: config, sessions: map[string]*userSession{}, slots: make(chan struct{}, config.ModelConcurrency), productGeneration: randomID()}
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, err error) {
	failMessage(w, status, err, publicError(err))
}

func failMessage(w http.ResponseWriter, status int, err error, message string) {
	code := "INVALID_REQUEST"
	var app *AppError
	if errors.As(err, &app) {
		code = app.ErrorCode()
	}
	w.Header().Set("X-Error-Code", code)
	respond(w, status, map[string]string{"error": message, "code": code})
}
func body(r *http.Request, dest any) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 1<<20 {
		return userError("请求超过 1 MiB 限制")
	}
	return strictJSON(data, dest)
}
func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/v1/") {
		s.productHTTP(w, r)
		return
	}
	if s.config.ProductAuth != nil {
		fail(w, 404, userError("产品服务模式仅开放 /api/v1 接口"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != "GET" && r.Method != "POST" {
		fail(w, 405, userError("不支持的请求方法"))
		return
	}
	if r.Method == "POST" {
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			fail(w, 403, userError("拒绝跨站修改请求"))
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			if err != nil || u.Host != r.Host || u.Scheme != scheme {
				fail(w, 403, userError("拒绝跨站修改请求"))
				return
			}
		}
	}
	if r.URL.Path == "/api/session" && r.Method == "POST" {
		u := s.find(r)
		if u == nil {
			u = s.createSession(w, r)
			if u == nil {
				return
			}
		}
		respond(w, 200, map[string]any{"model_available": s.config.Model != nil, "max_history": maxDialogTurns, "limits": map[string]any{"max_text_chars": maxTextChars, "session_idle_seconds": int(s.config.SessionIdleTTL.Seconds())}})
		return
	}
	if r.URL.Path == "/api/sample" && r.Method == "GET" {
		if len(s.config.Sample) == 0 {
			fail(w, 404, userError("无样例"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(s.config.Sample)
		return
	}
	u := s.find(r)
	if u == nil {
		fail(w, 401, userError("会话不存在，请刷新页面重新连接"))
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
			fail(w, 400, userError("请明确提供插入起点"))
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
		fail(w, 404, userError("接口不存在或请求方法不匹配"))
	}
}
