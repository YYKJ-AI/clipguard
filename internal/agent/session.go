package agent

import (
	"net"
	"net/http"
	"time"
)

const sessionCreationBurst = 20

type creationWindow struct {
	start time.Time
	count int
}

// Session metadata is guarded by Server.mu. Expiry is swept on requests so
// idle servers need no background goroutines or shutdown coordination.
func (s *Server) sweepSessions(now time.Time) {
	for id, u := range s.sessions {
		if now.Sub(u.lastSeen) >= s.config.SessionIdleTTL {
			delete(s.sessions, id)
		}
	}
	for ip, window := range s.creationWindows {
		if now.Sub(window.start) >= time.Minute {
			delete(s.creationWindows, ip)
		}
	}
}

func (s *Server) find(r *http.Request) *userSession {
	cookie, err := r.Cookie("safety_session")
	if err != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.config.Now()
	s.sweepSessions(now)
	u := s.sessions[cookie.Value]
	if u != nil {
		u.lastSeen = now
	}
	return u
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) *userSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.config.Now()
	s.sweepSessions(now)
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	window := s.creationWindows[ip]
	if window.count >= sessionCreationBurst || (window.count == 0 && len(s.creationWindows) >= s.config.MaxSessions*2) {
		w.Header().Set("Retry-After", "60")
		fail(w, http.StatusTooManyRequests, userError("新建会话过于频繁，请一分钟后重试"))
		return nil
	}
	if window.count == 0 {
		window.start = now
	}
	window.count++
	s.creationWindows[ip] = window
	if len(s.sessions) >= s.config.MaxSessions {
		var oldest string
		var seen time.Time
		for id, u := range s.sessions {
			if oldest == "" || u.lastSeen.Before(seen) {
				oldest, seen = id, u.lastSeen
			}
		}
		delete(s.sessions, oldest)
	}
	id := randomID()
	u := &userSession{workspace: newWorkspace(s.config.Now), lastSeen: now}
	s.sessions[id] = u
	http.SetCookie(w, &http.Cookie{Name: "safety_session", Value: id, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode})
	return u
}
