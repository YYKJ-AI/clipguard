package agent

import (
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"
)

// NewBearerAuthenticator loads an explicit token-to-user mapping for a product
// backend integration. Existing product identity systems can implement
// Authenticator instead; browsers must not be given a shared backend token.
func NewBearerAuthenticator(raw []byte) (Authenticator, error) {
	var config struct {
		Credentials []struct {
			Subject string `json:"subject"`
			Token   string `json:"token"`
		} `json:"credentials"`
	}
	if len(raw) > 1<<20 || strictJSON(raw, &config) != nil || len(config.Credentials) == 0 || len(config.Credentials) > 10000 {
		return nil, errors.New("身份配置无效，需要 1—10000 项 credentials")
	}
	auth := &bearerAuthenticator{subjects: map[[32]byte]string{}}
	for _, credential := range config.Credentials {
		if !validID(credential.Subject) || strings.TrimSpace(credential.Subject) != credential.Subject || len(credential.Token) < 32 || len(credential.Token) > 256 {
			return nil, errors.New("用户标识或凭据长度无效，凭据需要 32—256 个 ASCII 字符")
		}
		for _, c := range credential.Token {
			if c < 33 || c > 126 {
				return nil, errors.New("凭据不能包含空白或非 ASCII 字符")
			}
		}
		digest := sha256.Sum256([]byte(credential.Token))
		if _, exists := auth.subjects[digest]; exists {
			return nil, errors.New("身份配置存在重复凭据")
		}
		auth.subjects[digest] = credential.Subject
	}
	return auth, nil
}

type bearerAuthenticator struct{ subjects map[[32]byte]string }

func (a *bearerAuthenticator) Authenticate(r *http.Request) (string, error) {
	fields := strings.Fields(r.Header.Get("Authorization"))
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return "", errors.New("凭据无效")
	}
	if len(fields[1]) < 32 || len(fields[1]) > 256 {
		return "", errors.New("凭据无效")
	}
	subject, ok := a.subjects[sha256.Sum256([]byte(fields[1]))]
	if !ok {
		return "", errors.New("凭据无效")
	}
	return subject, nil
}
