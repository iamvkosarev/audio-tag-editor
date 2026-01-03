package app

import (
	"context"
	"github.com/iamvkosarev/music-tag-editor/internal/service/audio"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/iamvkosarev/music-tag-editor/internal/config"
	"github.com/iamvkosarev/music-tag-editor/internal/handler"
	"github.com/iamvkosarev/music-tag-editor/internal/server"
)

type App struct {
	server *server.Server
	config *config.Config
}

func New(cfg *config.Config) *App {
	audioService := audio.NewAudioService()

	h := handler.New(audioService)

	srv := server.New(cfg, h)

	return &App{
		server: srv,
		config: cfg,
	}
}

func (a *App) Run() {
	go func() {
		if err := a.server.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("failed to start server: %v", err)
		}
	}()

	log.Printf("server started on %s", a.config.Server.Address())

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := a.server.Shutdown(ctx); err != nil {
		log.Fatalf("server forced to shutdown: %v", err)
	}

	log.Println("server exited")
}
