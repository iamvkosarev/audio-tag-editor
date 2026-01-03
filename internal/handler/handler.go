package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/iamvkosarev/music-tag-editor/internal/model"
	"github.com/iamvkosarev/music-tag-editor/internal/templates"
)

type AudioService interface {
	ParseFile(filePath string) (*model.FileMetadata, error)
}

type Handler struct {
	audioService AudioService
}

func New(audioService AudioService) *Handler {
	return &Handler{
		audioService: audioService,
	}
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

func (h *Handler) Upload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		err := r.ParseMultipartForm(100 << 20)
		if err != nil {
			http.Error(w, "Failed to parse multipart form", http.StatusBadRequest)
			return
		}

		files := r.MultipartForm.File["files"]
		if len(files) == 0 {
			http.Error(w, "No files provided", http.StatusBadRequest)
			return
		}

		var fileMetadata []model.FileMetadata

		for _, fileHeader := range files {
			file, err := fileHeader.Open()
			if err != nil {
				continue
			}

			tempFile, err := os.CreateTemp("", "audio-*"+filepath.Ext(fileHeader.Filename))
			if err != nil {
				file.Close()
				continue
			}

			_, err = io.Copy(tempFile, file)
			file.Close()
			if err != nil {
				tempFile.Close()
				os.Remove(tempFile.Name())
				continue
			}
			tempFile.Close()

			metadata, err := h.audioService.ParseFile(tempFile.Name())
			if err == nil {
				fileMetadata = append(fileMetadata, *metadata)
			}

			os.Remove(tempFile.Name())
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"files": fileMetadata,
		})
	}
}
