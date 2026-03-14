package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Config holds all runtime configuration, loaded from environment variables.
type Config struct {
	AccessKey  string
	Recipient  string
	Domain     string
	BridgeKey  string
	BaseURL    string
	ListenAddr string
}

func loadConfig() Config {
	required := map[string]string{
		"MAILX_ACCESS_KEY": "",
		"MAILX_RECIPIENT":  "",
		"MAILX_DOMAIN":     "",
		"BRIDGE_API_KEY":   "",
	}
	for k := range required {
		v := os.Getenv(k)
		if v == "" {
			log.Fatalf("required environment variable %s is not set", k)
		}
		required[k] = v
	}

	baseURL := os.Getenv("MAILX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.mailx.net/v1"
	}

	listenAddr := os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":8080"
	}

	return Config{
		AccessKey:  required["MAILX_ACCESS_KEY"],
		Recipient:  required["MAILX_RECIPIENT"],
		Domain:     required["MAILX_DOMAIN"],
		BridgeKey:  required["BRIDGE_API_KEY"],
		BaseURL:    baseURL,
		ListenAddr: listenAddr,
	}
}

func main() {
	cfg := loadConfig()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	slog.Info("startup config",
		"base_url", cfg.BaseURL,
		"domain", cfg.Domain,
		"recipient", cfg.Recipient,
		"listen_addr", cfg.ListenAddr,
	)

	client := NewMailxClient(cfg, nil)

	slog.Info("authenticating with Mailx API")
	if err := client.Authenticate(context.Background()); err != nil {
		log.Fatalf("failed to authenticate with Mailx API: %v", err)
	}
	slog.Info("Mailx authentication successful")

	mux := http.NewServeMux()
	registerHandlers(mux, client)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 16, // 64 KiB
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("starting HTTP server", "addr", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server shutdown error: %v", err)
	}
	slog.Info("shutdown complete")
}
