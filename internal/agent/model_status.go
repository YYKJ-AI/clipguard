package agent

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Status describes a real protocol check, not a claim about arbitrary semantics.
type ModelStatus struct {
	State     string `json:"state"`
	JSON      bool   `json:"json_verified"`
	Stream    bool   `json:"stream_verified"`
	CheckedAt string `json:"checked_at,omitempty"`
	Message   string `json:"message"`
}

func (s *Server) ModelStatus() ModelStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config.Model == nil {
		return ModelStatus{State: "unconfigured", Message: "在线模型未配置，离线功能完整可用。"}
	}
	if s.modelStatus.State == "" {
		return ModelStatus{State: "pending", Message: "在线模型已配置，协议待验证；自由表达效果需另行验收。"}
	}
	return s.modelStatus
}
func (s *Server) checkModel(w http.ResponseWriter, r *http.Request) {
	if s.config.Model == nil {
		fail(w, 503, errors.New("在线模型未配置，离线功能完整可用"))
		return
	}
	if !s.modelCheckMu.TryLock() {
		fail(w, 409, errors.New("模型连接检查正在进行，请稍后重试"))
		return
	}
	defer s.modelCheckMu.Unlock()
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		fail(w, 503, errors.New("模型服务繁忙，请稍后重试"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.config.ModelTimeout)
	defer cancel()
	status := ModelStatus{State: "unavailable", CheckedAt: time.Now().UTC().Format(time.RFC3339)}
	p := Project{ID: "connection-check", Revision: 1, Tracks: []Track{{ID: "video", Kind: "video"}}, Clips: []Clip{}}
	turns := []Turn{{Role: "user", Content: "请简短说明你支持哪些剪辑操作。这是连接检查，只返回咨询，不生成编辑计划。"}}
	for _, stream := range []bool{false, true} {
		attempt := ctx
		if stream {
			attempt = context.WithValue(ctx, streamSinkKey{}, streamSink(func(string, any) {}))
		}
		result, err := s.config.Model.Parse(attempt, p, turns)
		if err == nil {
			err = validateParseResult(p, result)
		}
		if err == nil && result.Status != "answer" {
			err = errors.New("连接检查未收到咨询回复，请检查模型配置")
		}
		if err != nil {
			status.Message = publicError(err) + "。检查没有修改任何工程，可继续使用离线功能。"
			break
		}
		if stream {
			status.Stream = true
		} else {
			status.JSON = true
		}
	}
	if status.JSON && status.Stream {
		status.State = "connected"
		status.Message = "普通回复与流式协议已连通；这不代表任意剪辑指令均已适配。"
	}
	s.mu.Lock()
	s.modelStatus = status
	s.mu.Unlock()
	respond(w, 200, status)
}
