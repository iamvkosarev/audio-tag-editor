package server

import (
	"context"
	"net/http"

	"github.com/iamvkosarev/audio-tag-editor/internal/config"
	"github.com/iamvkosarev/audio-tag-editor/internal/handler"
)

type Server struct {
	httpServer *http.Server
	config     *config.ServerConfig
}

func New(cfg *config.Config, h *handler.Handler) *Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", h.Index)
	mux.HandleFunc("POST /api/upload", h.Upload)
	mux.HandleFunc("POST /api/update-tags", h.UpdateTags)
	mux.HandleFunc("GET /api/download/", h.Download)
	mux.HandleFunc("GET /api/download-all", h.DownloadAll)
	mux.HandleFunc("POST /api/download-selected", h.DownloadSelected)

	srv := &http.Server{
		Addr:         cfg.Server.Address(),
		Handler:      mux,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	return &Server{
		httpServer: srv,
		config:     &cfg.Server,
	}
}

func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
