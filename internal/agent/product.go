package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// Authenticator returns a stable, verified product user identifier. It must not
// trust a user ID supplied by the browser without validating its credentials.
type Authenticator interface {
	Authenticate(*http.Request) (string, error)
}

// ProjectCodec is the boundary for a future product project format. The engine
// and mutation responses always use the canonical timeline.v1 representation.
// Decode must reject unsupported/lossy inputs; Encode is used only by export.
type ProjectCodec interface {
	Name() string
	Decode([]byte) (Project, error)
	Encode(Project) ([]byte, error)
}

type TimelineCodec struct{}

func (TimelineCodec) Name() string                        { return "timeline.v1" }
func (TimelineCodec) Decode(data []byte) (Project, error) { return DecodeProject(data) }
func (TimelineCodec) Encode(p Project) ([]byte, error) {
	if err := Validate(p); err != nil {
		return nil, err
	}
	return json.Marshal(p)
}

type projectKey struct{ Owner, Project string }
type productError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Retryable bool   `json:"retryable"`
}

func (s *Server) productHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if w.Header().Get("X-Request-ID") == "" {
		w.Header().Set("X-Request-ID", randomID())
	}
	if s.config.ProductAuth == nil {
		productFail(w, 503, "INTEGRATION_DISABLED", errors.New("产品接口尚未配置身份校验"), false)
		return
	}
	owner, err := s.config.ProductAuth.Authenticate(r)
	if err != nil || !validID(owner) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="safetyagent"`)
		productFail(w, 401, "UNAUTHENTICATED", errors.New("需要有效的产品身份凭据"), false)
		return
	}
	if r.Method != "GET" && r.Method != "POST" && r.Method != "PUT" {
		w.Header().Set("Allow", "GET, POST, PUT")
		productFail(w, 405, "METHOD_NOT_ALLOWED", errors.New("不支持的请求方法"), false)
		return
	}
	if r.Method != "GET" {
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			productFail(w, 403, "FORBIDDEN", errors.New("拒绝跨站修改请求"), false)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			if err != nil || u.Host != r.Host || u.Scheme != scheme {
				productFail(w, 403, "FORBIDDEN", errors.New("拒绝跨站修改请求"), false)
				return
			}
		}
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			productFail(w, 415, "UNSUPPORTED_MEDIA_TYPE", errors.New("请求需要 application/json"), false)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
		if err != nil {
			productFail(w, 400, "INVALID_REQUEST", errors.New("无法读取请求"), false)
			return
		}
		if len(raw) > 1<<20 {
			productFail(w, 413, "PAYLOAD_TOO_LARGE", errors.New("请求超过 1 MiB"), false)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/")
	if len(parts) < 2 || parts[0] != "projects" || !validID(parts[1]) {
		productFail(w, 404, "NOT_FOUND", errors.New("接口不存在"), false)
		return
	}
	projectID := parts[1]
	codec := s.config.ProjectCodec
	if codec == nil {
		codec = TimelineCodec{}
	}
	if len(parts) == 2 && r.Method == "PUT" {
		s.productWrite(w, r, owner, projectID, codec)
		return
	}
	s.mu.Lock()
	u := s.productProjects[projectKey{owner, projectID}]
	s.mu.Unlock()
	if u == nil {
		productFail(w, 404, "PROJECT_NOT_FOUND", errors.New("当前用户的工程不存在"), false)
		return
	}
	if len(parts) == 3 && parts[2] == "history" && r.Method == "GET" {
		respond(w, 200, u.workspace.History())
		return
	}
	if r.Method == "GET" && (len(parts) == 2 || (len(parts) == 3 && parts[2] == "export")) {
		p, b, err := u.workspace.Snapshot()
		if err != nil {
			productFail(w, 404, "PROJECT_NOT_FOUND", errors.New("当前用户的工程不存在"), false)
			return
		}
		w.Header().Set("ETag", s.productETag(b))
		if len(parts) == 3 {
			raw, err := codec.Encode(p)
			if err != nil || !json.Valid(raw) {
				productFail(w, 500, "PROJECT_ENCODING_FAILED", errors.New("工程导出适配器失败，工程未修改"), false)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Content-Disposition", `attachment; filename="timeline.json"`)
			w.WriteHeader(200)
			_, _ = w.Write(raw)
			return
		}
		respond(w, 200, map[string]any{"project": p, "format": "timeline.v1", "source_format": codec.Name()})
		return
	}
	if r.Method == "POST" {
		if len(parts) == 3 && parts[2] == "messages" {
			capture := newProductCapture()
			s.message(capture, r, u)
			s.flushProduct(w, capture)
			return
		}
		if len(parts) == 3 && (parts[2] == "undo" || parts[2] == "redo") {
			var input struct {
				RequestID string `json:"request_id"`
			}
			if err := body(r, &input); err != nil {
				productFail(w, 400, "INVALID_REQUEST", err, false)
				return
			}
			var result Result
			var err error
			if parts[2] == "redo" {
				result, err = u.workspace.Redo(input.RequestID)
			} else {
				result, err = u.workspace.Undo(input.RequestID)
			}
			if err != nil {
				productFail(w, 409, "STATE_CONFLICT", err, false)
				return
			}
			u.clearRecentCaption()
			respond(w, 200, result)
			return
		}
		if len(parts) == 5 && parts[2] == "plans" && validID(parts[3]) && (parts[4] == "confirm" || parts[4] == "cancel") {
			var input struct{}
			if err := body(r, &input); err != nil {
				productFail(w, 400, "INVALID_REQUEST", err, false)
				return
			}
			if parts[4] == "confirm" {
				result, err := u.workspace.Confirm(parts[3])
				if err != nil {
					productFail(w, 409, "STATE_CONFLICT", err, false)
					return
				}
				u.captureRecentCaption(result)
				respond(w, 200, result)
				return
			}
			if err := u.workspace.Cancel(parts[3]); err != nil {
				productFail(w, 409, "STATE_CONFLICT", err, false)
				return
			}
			respond(w, 200, map[string]string{"message": "已取消，工程没有变化"})
			return
		}
	}
	productFail(w, 404, "NOT_FOUND", errors.New("接口不存在或请求方法不匹配"), false)
}

func (s *Server) productETag(b Binding) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%d/%d", s.productGeneration, b.ProjectID, b.Epoch, b.Revision)))
	return fmt.Sprintf(`"%x"`, sum[:16])
}

func (s *Server) productWrite(w http.ResponseWriter, r *http.Request, owner, id string, codec ProjectCodec) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil {
		productFail(w, 400, "INVALID_REQUEST", errors.New("无法读取请求"), false)
		return
	}
	if len(raw) > 1<<20 {
		productFail(w, 413, "PAYLOAD_TOO_LARGE", errors.New("工程请求超过 1 MiB"), false)
		return
	}
	p, err := codec.Decode(raw)
	if err != nil {
		productFail(w, 422, "INVALID_PROJECT", err, false)
		return
	}
	if err := Validate(p); err != nil {
		productFail(w, 422, "INVALID_PROJECT", err, false)
		return
	}
	if p.ID != id {
		productFail(w, 422, "PROJECT_ID_MISMATCH", errors.New("路径工程 ID 与请求工程 ID 不一致"), false)
		return
	}
	none, match := r.Header.Get("If-None-Match"), r.Header.Get("If-Match")
	if none == "" && match == "" {
		productFail(w, 428, "PRECONDITION_REQUIRED", errors.New("创建需要 If-None-Match: *，替换需要读取到的 If-Match"), false)
		return
	}
	if (none != "" && none != "*") || (none != "" && match != "") {
		productFail(w, 400, "INVALID_REQUEST", errors.New("条件请求头无效"), false)
		return
	}
	key := projectKey{owner, id}
	s.mu.Lock()
	if s.productProjects == nil {
		s.productProjects = map[projectKey]*userSession{}
	}
	u := s.productProjects[key]
	if u == nil && none == "*" {
		if len(s.productProjects) >= s.config.MaxSessions {
			s.mu.Unlock()
			productFail(w, 503, "PROJECT_CAPACITY_EXCEEDED", errors.New("工程容量已满，已有工程不会被清理"), false)
			return
		}
		u = &userSession{workspace: newWorkspace(s.config.Now)}
		s.productProjects[key] = u
	}
	s.mu.Unlock()
	if u == nil {
		productFail(w, 412, "PRECONDITION_FAILED", errPrecondition, false)
		return
	}
	_, binding, snapshotErr := u.workspace.Snapshot()
	exists := snapshotErr == nil
	if (none == "*" && exists) || (match != "" && (!exists || match != s.productETag(binding))) {
		productFail(w, 412, "PRECONDITION_FAILED", errPrecondition, false)
		return
	}
	next, err := u.workspace.compareAndLoad(binding, p)
	if err != nil {
		productFail(w, 412, "PRECONDITION_FAILED", err, false)
		return
	}
	status := 200
	if !exists {
		status = 201
	}
	w.Header().Set("ETag", s.productETag(next))
	respond(w, status, map[string]any{"project": p, "format": "timeline.v1", "source_format": codec.Name(), "message": "工程已加载，旧计划和撤销历史已清空"})
}

// The legacy browser and v1 API share the same coordinator, while each keeps
// its response contract. This buffer contains only a bounded JSON response.
type productCapture struct {
	header http.Header
	status int
	bytes.Buffer
}

func newProductCapture() *productCapture         { return &productCapture{header: make(http.Header)} }
func (c *productCapture) Header() http.Header    { return c.header }
func (c *productCapture) WriteHeader(status int) { c.status = status }
func (c *productCapture) Write(p []byte) (int, error) {
	if c.status == 0 {
		c.status = 200
	}
	return c.Buffer.Write(p)
}
func (s *Server) flushProduct(w http.ResponseWriter, c *productCapture) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	if c.status >= 400 {
		var legacy struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(c.Bytes(), &legacy)
		code, retry := "INVALID_REQUEST", false
		switch c.status {
		case 409:
			code = "STATE_CONFLICT"
		case 422:
			code = "EDIT_REJECTED"
		case 503:
			code = "MODEL_UNAVAILABLE"
			if s.config.Model != nil {
				code = "MODEL_BUSY"
				retry = true
				w.Header().Set("Retry-After", "1")
			}
		case 504:
			code = "MODEL_TIMEOUT"
			retry = true
		}
		productFail(w, c.status, code, errors.New(legacy.Error), retry)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(c.status)
	_, _ = w.Write(c.Bytes())
}
func productFail(w http.ResponseWriter, status int, code string, err error, retryable bool) {
	w.Header().Set("X-Error-Code", code)
	respond(w, status, map[string]any{"error": productError{code, err.Error(), w.Header().Get("X-Request-ID"), retryable}})
}
