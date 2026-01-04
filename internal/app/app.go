package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/iamvkosarev/audio-tag-editor/internal/service/audio"
	"github.com/iamvkosarev/audio-tag-editor/pkg/logs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/iamvkosarev/audio-tag-editor/internal/config"
	"github.com/iamvkosarev/audio-tag-editor/internal/handler"
	"github.com/iamvkosarev/audio-tag-editor/internal/server"
)

type App struct {
	server *server.Server
	config *config.Config
}

func New(cfg *config.Config) (*App, error) {
	audioService := audio.NewAudioService()

	h := handler.New(audioService)

	srv := server.New(cfg, h)

	log, err := logs.NewSlogLogger(cfg.App.LogMode, os.Stdout)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize slog: %w", err)
	}
	slog.SetDefault(log)

	return &App{
		server: srv,
		config: cfg,
	}, nil
}

func (a *App) Run() error {
	ctx, cancel := context.WithCancel(context.Background())

	var joinedErr error

	go func() {
		slog.Info("start server", slog.String("address", a.config.Server.Address()))
		if err := a.server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			joinedErr = errors.Join(joinedErr, fmt.Errorf("failed to start server: %w", err))
			cancel()
		}
	}()

	slog.Info("start app")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-ctx.Done():
	case <-quit:
		cancel()
	}

	wg := sync.WaitGroup{}
	wgChan := make(chan struct{})

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), a.config.App.ShutdownTimeout)
	defer shutdownCancel()

	slog.Info("start shutdown")
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := a.server.Shutdown(shutdownCtx); err != nil {
			joinedErr = errors.Join(joinedErr, err)
		}
		slog.Info("stop server")
	}()

	go func() {
		defer close(wgChan)
		wg.Wait()
	}()

	select {
	case <-wgChan:
		slog.Info("stop shutdown")
	case <-shutdownCtx.Done():
		slog.Info("finish context: timeout")
	}

	slog.Info("stop app")
	return joinedErr
}
