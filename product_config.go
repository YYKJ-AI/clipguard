package main

import (
	"fmt"
	"io"
	"os"

	"safetyagent/internal/agent"
)

func loadProductAuth(path string) (agent.Authenticator, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("无法读取 PRODUCT_AUTH_FILE，请检查路径和权限")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("无法读取 PRODUCT_AUTH_FILE")
	}
	return agent.NewBearerAuthenticator(raw)
}
