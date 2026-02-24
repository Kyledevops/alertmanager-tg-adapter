package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alertmanager-tg-adapter/internal/bot"
	"github.com/alertmanager-tg-adapter/internal/config"
	"github.com/alertmanager-tg-adapter/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("❌ Failed to load config: %v", err)
	}

	tgBot, err := bot.New(cfg.TelegramToken, cfg)
	if err != nil {
		log.Fatalf("❌ Failed to initialize Telegram bot: %v", err)
	}

	log.Printf("🤖 Bot authorized as @%s", tgBot.API.Self.UserName)

	srv := server.New(cfg, tgBot)

	port := cfg.ListenAddr
	if port == "" {
		port = ":9087"
	}

	// Create HTTP server for graceful shutdown
	httpServer := &http.Server{
		Addr:    port,
		Handler: srv.Handler(),
	}

	// Start server in goroutine
	go func() {
		log.Printf("🚀 Alertmanager Telegram Adapter listening on %s", port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("❌ Server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit

	log.Printf("🛑 Received signal %v, shutting down gracefully...", sig)

	// Give outstanding requests 15 seconds to complete
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("❌ Server forced to shutdown: %v", err)
	}

	log.Println("✅ Server stopped gracefully")
	os.Exit(0)
}
