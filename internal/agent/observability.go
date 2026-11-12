package agent

import (
	"log/slog"
	"net/http"
	"time"
)

type observedResponse struct {
	http.ResponseWriter
	status int
}

func (w *observedResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *observedResponse) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *observedResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

// Never log request bodies, model output, bearer tokens or session cookies.
// ResponseController can unwrap the observer for real SSE flushing.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	id := randomID()
	w.Header().Set("X-Request-ID", id)
	observed := &observedResponse{ResponseWriter: w}
	defer func() {
		status := observed.status
		if status == 0 {
			status = http.StatusOK
		}
		level := slog.LevelInfo
		if status >= 400 || w.Header().Get("X-Error-Code") != "" {
			level = slog.LevelWarn
		}
		s.config.Logger.Log(r.Context(), level, "api_request", "request_id", id, "method", r.Method, "path", r.URL.Path, "status", status, "duration_ms", time.Since(started).Milliseconds(), "error_code", w.Header().Get("X-Error-Code"))
	}()
	s.serveHTTP(observed, r)
}
