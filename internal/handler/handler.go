package handler

import (
	"net/http"

	"github.com/iamvkosarev/music-tag-editor/internal/templates"
)

type Handler struct{}

func New() *Handler {
	return &Handler{}
}

func (h *Handler) Index() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		templates.Index().Render(r.Context(), w)
	}
}

