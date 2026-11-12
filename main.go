package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"safetyagent/internal/agent"
	"strconv"
	"strings"
	"time"
)

//go:embed web/* examples/timeline.json
var assets embed.FS

func application(config agent.Config) http.Handler {
	sample, err := assets.ReadFile("examples/timeline.json")
	if err != nil {
		panic(err)
	}
	config.Sample = sample
	api := agent.NewServer(config)
	web, err := fs.Sub(assets, "web")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(web))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "method not allowed", 405)
			return
		}
		if r.URL.Path != "/" && r.URL.Path != "/app.js" && r.URL.Path != "/style.css" && r.URL.Path != "/demo-cover.svg" {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	})
}
func envInt(name string, defaultValue int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		log.Fatalf("%s 必须是正整数", name)
	}
	return n
}
func main() {
	listen := os.Getenv("LISTEN_ADDR")
	if listen == "" {
		listen = "127.0.0.1:8090"
	}
	timeout := envInt("MODEL_TIMEOUT_SECONDS", 45)
	config := agent.Config{MaxSessions: envInt("MAX_SESSIONS", 128), ModelConcurrency: envInt("MODEL_CONCURRENCY", 4), ModelTimeout: time.Duration(timeout) * time.Second}
	productAuth, authErr := loadProductAuth(os.Getenv("PRODUCT_AUTH_FILE"))
	if authErr != nil {
		log.Fatal(authErr)
	}
	config.ProductAuth = productAuth
	if productAuth != nil {
		log.Print("产品 v1 接口已启用身份校验")
	}
	base, model := os.Getenv("LLM_BASE_URL"), os.Getenv("LLM_MODEL")
	if (base == "") != (model == "") {
		log.Fatal("LLM_BASE_URL 与 LLM_MODEL 必须同时配置")
	}
	if base != "" {
		config.Model = agent.HTTPModel{BaseURL: base, Model: model, APIKey: os.Getenv("LLM_API_KEY"), JSONMode: os.Getenv("LLM_JSON_MODE") == "true", ThinkingMode: os.Getenv("LLM_THINKING"), MaxTokens: envInt("LLM_MAX_TOKENS", 4096)}
		log.Print("模型模式已配置；实际可用性需通过调用验证")
	} else {
		log.Print("未配置模型，离线中文句式可用")
	}
	server := &http.Server{Addr: listen, Handler: application(config), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: (time.Duration(timeout) + 15) * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Printf("安全剪辑工作台：http://%s\n按 Ctrl+C 停止。\n", listen)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
