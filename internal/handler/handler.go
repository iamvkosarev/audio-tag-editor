package handler

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/iamvkosarev/music-tag-editor/internal/model"
	"github.com/iamvkosarev/music-tag-editor/internal/templates"
)

type AudioService interface {
	ParseFile(filePath string) (*model.FileMetadata, error)
	UpdateTags(filePath string, title, artist, album *string, year, track *int, genre *string, coverArt *string) error
}

type storedFile struct {
	Path      string
	Filename  string
	Metadata  *model.FileMetadata
	ExpiresAt time.Time
}

type Handler struct {
	audioService AudioService
	files        map[string]*storedFile
	mu           sync.RWMutex
}

func New(audioService AudioService) *Handler {
	h := &Handler{
		audioService: audioService,
		files:        make(map[string]*storedFile),
	}
	go h.cleanupExpiredFiles()
	return h
}

func (h *Handler) cleanupExpiredFiles() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		h.mu.Lock()
		now := time.Now()
		for id, file := range h.files {
			if now.After(file.ExpiresAt) {
				os.Remove(file.Path)
				delete(h.files, id)
			}
		}
		h.mu.Unlock()
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
				fileID := uuid.New().String()
				metadata.ID = fileID

				h.mu.Lock()
				h.files[fileID] = &storedFile{
					Path:      tempFile.Name(),
					Filename:  fileHeader.Filename,
					Metadata:  metadata,
					ExpiresAt: time.Now().Add(24 * time.Hour),
				}
				h.mu.Unlock()

				fileMetadata = append(fileMetadata, *metadata)
			} else {
				os.Remove(tempFile.Name())
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(
			map[string]interface{}{
				"files": fileMetadata,
			},
		)
	}
}

func (h *Handler) UpdateTags() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Handler.UpdateTags: Request received: Method=%s, Path=%s", r.Method, r.URL.Path)

		if r.Method != http.MethodPost {
			log.Printf("Handler.UpdateTags: Method not allowed: %s", r.Method)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req model.TagUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("Handler.UpdateTags: Failed to decode request body: %v", err)
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		log.Printf(
			"Handler.UpdateTags: Request decoded: FileIds=%d, Title=%v, Artist=%v, Album=%v, Year=%v, Track=%v, Genre=%v",
			len(req.FileIds), req.Title != nil, req.Artist != nil, req.Album != nil, req.Year != nil, req.Track != nil,
			req.Genre != nil,
		)
		log.Printf("Handler.UpdateTags: File IDs: %v", req.FileIds)

		if len(req.FileIds) == 0 {
			log.Printf("Handler.UpdateTags: No file IDs provided")
			http.Error(w, "No file IDs provided", http.StatusBadRequest)
			return
		}

		var updatedFiles []model.FileMetadata
		var errors []string

		log.Printf("Handler.UpdateTags: About to acquire read lock...")
		h.mu.RLock()
		log.Printf("Handler.UpdateTags: Read lock acquired, Total stored files: %d", len(h.files))
		for fileID, stored := range h.files {
			log.Printf("Handler.UpdateTags: Stored file: ID=%s, Path=%s", fileID, stored.Path)
		}

		filePaths := make(map[string]string)
		for _, fileID := range req.FileIds {
			stored, exists := h.files[fileID]
			if !exists {
				errMsg := fmt.Sprintf("file %s not found", fileID)
				log.Printf("Handler.UpdateTags: %s", errMsg)
				errors = append(errors, errMsg)
				continue
			}
			filePaths[fileID] = stored.Path
		}
		h.mu.RUnlock()
		log.Printf("Handler.UpdateTags: Read lock released, processing %d files", len(filePaths))

		for fileID, filePath := range filePaths {
			log.Printf("Handler.UpdateTags: Processing file: ID=%s, Path=%s", fileID, filePath)

			err := h.audioService.UpdateTags(filePath, req.Title, req.Artist, req.Album, req.Year, req.Track, req.Genre, req.CoverArt)
			if err != nil {
				errMsg := fmt.Sprintf("file %s: %v", fileID, err)
				log.Printf("Handler.UpdateTags: Error updating tags: %s", errMsg)
				errors = append(errors, errMsg)
				continue
			}

			log.Printf("Handler.UpdateTags: Tags updated successfully for file: %s", fileID)

			var metadata *model.FileMetadata
			var parseErr error

			metadata, parseErr = h.audioService.ParseFile(filePath)

			if parseErr != nil {
				errMsg := fmt.Sprintf("file %s: failed to re-parse: %v", fileID, parseErr)
				log.Printf("Handler.UpdateTags: Error re-parsing file: %s", errMsg)
				errors = append(errors, errMsg)
				continue
			}
			metadata.ID = fileID
			updatedFiles = append(updatedFiles, *metadata)

			h.mu.Lock()
			if stored, exists := h.files[fileID]; exists {
				stored.Metadata = metadata
			}
			h.mu.Unlock()

			log.Printf(
				"Handler.UpdateTags: File re-parsed successfully: ID=%s, Artist=%s, Album=%s, Genre=%s",
				fileID, metadata.Artist, metadata.Album, metadata.Genre,
			)
		}

		log.Printf("Handler.UpdateTags: Processing complete: Updated=%d, Errors=%d", len(updatedFiles), len(errors))

		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{
			"files": updatedFiles,
		}
		if len(updatedFiles) == 0 {
			response["files"] = []model.FileMetadata{}
		}
		if len(errors) > 0 {
			response["errors"] = errors
			log.Printf("Handler.UpdateTags: Errors in response: %v", errors)
		}

		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Handler.UpdateTags: Failed to encode response: %v", err)
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}

		log.Printf("Handler.UpdateTags: Response sent successfully: Files=%d", len(updatedFiles))
	}
}

func (h *Handler) Download() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		fileID := strings.TrimPrefix(r.URL.Path, "/api/download/")
		if fileID == "" {
			http.Error(w, "File ID required", http.StatusBadRequest)
			return
		}

		h.mu.RLock()
		stored, exists := h.files[fileID]
		h.mu.RUnlock()

		if !exists {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}

		filePath, cleanup, err := h.prepareFileWithCoverArt(stored)
		if err != nil {
			log.Printf("Handler.Download: Failed to prepare file with cover art: %v, using original file", err)
			filePath = stored.Path
			cleanup = func() {}
		}
		defer func() {
			if cleanup != nil {
				cleanup()
			}
		}()

		if _, err := os.Stat(filePath); err != nil {
			log.Printf("Handler.Download: File does not exist: %v", err)
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}

		file, err := os.Open(filePath)
		if err != nil {
			log.Printf("Handler.Download: Failed to open file: %v", err)
			http.Error(w, "Failed to open file", http.StatusInternalServerError)
			return
		}
		defer file.Close()

		stat, err := file.Stat()
		if err != nil {
			log.Printf("Handler.Download: Failed to stat file: %v", err)
			http.Error(w, "Failed to stat file", http.StatusInternalServerError)
			return
		}

		downloadFilename := h.buildDownloadFilename(stored)

		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", downloadFilename))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))

		io.Copy(w, file)
		log.Printf("Handler.Download: File downloaded: ID=%s, Filename=%s", fileID, downloadFilename)
	}
}

func (h *Handler) buildDownloadFilename(stored *storedFile) string {
	if stored.Metadata == nil {
		return stored.Filename
	}

	meta := stored.Metadata
	var filename string

	artist := meta.Artist
	album := meta.Album
	disc := meta.Disc
	track := meta.Track
	title := meta.Title

	if title == "" {
		title = stored.Filename
		ext := filepath.Ext(title)
		title = strings.TrimSuffix(title, ext)
	}

	parts := []string{}
	if artist != "" {
		parts = append(parts, artist)
	}
	if album != "" {
		parts = append(parts, album)
	}

	discTrackPart := ""
	if track > 0 {
		if disc > 0 {
			discTrackPart = fmt.Sprintf("%d-%02d", disc, track)
		} else {
			discTrackPart = fmt.Sprintf("%02d", track)
		}
	}

	if discTrackPart != "" {
		parts = append(parts, discTrackPart)
	}

	if len(parts) > 0 {
		filename = strings.Join(parts, " - ")
		if title != "" {
			filename += " " + title
		}
	} else {
		if discTrackPart != "" && title != "" {
			filename = discTrackPart + " " + title
		} else {
			filename = title
		}
	}
	if filename == "" {
		filename = stored.Filename
	}

	ext := filepath.Ext(stored.Filename)
	if ext != "" && !strings.HasSuffix(filename, ext) {
		filename += ext
	}

	filename = sanitizeFilename(filename)
	if filename == "" {
		filename = stored.Filename
	}

	return filename
}

func sanitizeFilename(filename string) string {
	invalidChars := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	result := filename
	for _, char := range invalidChars {
		result = strings.ReplaceAll(result, char, "_")
	}
	result = strings.TrimSpace(result)
	return result
}

func (h *Handler) prepareFileWithCoverArt(stored *storedFile) (string, func(), error) {
	if stored.Metadata == nil || stored.Metadata.CoverArt == "" {
		return stored.Path, func() {}, nil
	}

	sourceFile, err := os.Open(stored.Path)
	if err != nil {
		return stored.Path, func() {}, fmt.Errorf("failed to open source file: %w", err)
	}
	defer sourceFile.Close()

	sourceStat, err := sourceFile.Stat()
	if err != nil {
		return stored.Path, func() {}, fmt.Errorf("failed to stat source file: %w", err)
	}
	originalModTime := sourceStat.ModTime()

	tempFile, err := os.CreateTemp("", "download-*"+filepath.Ext(stored.Path))
	if err != nil {
		return stored.Path, func() {}, fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	tempFile.Close()

	destFile, err := os.Create(tempPath)
	if err != nil {
		os.Remove(tempPath)
		return stored.Path, func() {}, fmt.Errorf("failed to create dest file: %w", err)
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		destFile.Close()
		os.Remove(tempPath)
		return stored.Path, func() {}, fmt.Errorf("failed to copy file: %w", err)
	}
	destFile.Close()

	coverArt := stored.Metadata.CoverArt
	updateErr := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Handler.prepareFileWithCoverArt: Panic while embedding cover art: %v", r)
				err = fmt.Errorf("panic while embedding cover art: %v", r)
			}
		}()
		return h.audioService.UpdateTags(tempPath, nil, nil, nil, nil, nil, nil, &coverArt)
	}()
	if updateErr != nil {
		os.Remove(tempPath)
		log.Printf("Handler.prepareFileWithCoverArt: Failed to embed cover art: %v", updateErr)
		return stored.Path, func() {}, fmt.Errorf("failed to embed cover art: %w", updateErr)
	}

	if err := os.Chtimes(tempPath, originalModTime, originalModTime); err != nil {
		log.Printf("Handler.prepareFileWithCoverArt: Failed to set modification time: %v", err)
	}

	log.Printf("Handler.prepareFileWithCoverArt: Successfully embedded cover art for %s", stored.Path)

	cleanup := func() {
		os.Remove(tempPath)
	}

	return tempPath, cleanup, nil
}

func (h *Handler) DownloadAll() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		h.mu.RLock()
		filesToZip := make([]*storedFile, 0, len(h.files))
		for _, stored := range h.files {
			filesToZip = append(filesToZip, stored)
		}
		h.mu.RUnlock()

		if len(filesToZip) == 0 {
			http.Error(w, "No files to download", http.StatusNotFound)
			return
		}

		zipFilename := h.buildZipFilename(filesToZip)

		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", zipFilename))

		zipWriter := zip.NewWriter(w)
		defer zipWriter.Close()

		for _, stored := range filesToZip {
			filePath, cleanup, err := h.prepareFileWithCoverArt(stored)
			if err != nil {
				log.Printf("Handler.DownloadAll: Failed to prepare file %s: %v, using original file", stored.Path, err)
				filePath = stored.Path
				cleanup = func() {}
			}

			if _, err := os.Stat(filePath); err != nil {
				if cleanup != nil {
					cleanup()
				}
				log.Printf("Handler.DownloadAll: File does not exist %s: %v", filePath, err)
				continue
			}

			file, err := os.Open(filePath)
			if err != nil {
				if cleanup != nil {
					cleanup()
				}
				log.Printf("Handler.DownloadAll: Failed to open file %s: %v", filePath, err)
				continue
			}

			fileStat, err := file.Stat()
			if err != nil {
				file.Close()
				if cleanup != nil {
					cleanup()
				}
				log.Printf("Handler.DownloadAll: Failed to stat file %s: %v", filePath, err)
				continue
			}

			downloadFilename := h.buildDownloadFilename(stored)
			zipHeader := &zip.FileHeader{
				Name:               downloadFilename,
				Method:             zip.Deflate,
				Modified:           fileStat.ModTime(),
				UncompressedSize64: uint64(fileStat.Size()),
			}
			zipEntry, err := zipWriter.CreateHeader(zipHeader)
			if err != nil {
				file.Close()
				if cleanup != nil {
					cleanup()
				}
				log.Printf("Handler.DownloadAll: Failed to create zip entry for %s: %v", downloadFilename, err)
				continue
			}

			_, err = io.Copy(zipEntry, file)
			file.Close()
			if cleanup != nil {
				cleanup()
			}
			if err != nil {
				log.Printf("Handler.DownloadAll: Failed to write file %s to zip: %v", downloadFilename, err)
				continue
			}
		}

		log.Printf("Handler.DownloadAll: ZIP file created with %d files", len(filesToZip))
	}
}

func (h *Handler) DownloadSelected() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			FileIds []string `json:"fileIds"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("Handler.DownloadSelected: Failed to decode request: %v", err)
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if len(req.FileIds) == 0 {
			http.Error(w, "No file IDs provided", http.StatusBadRequest)
			return
		}

		h.mu.RLock()
		filesToZip := make([]*storedFile, 0, len(req.FileIds))
		for _, fileID := range req.FileIds {
			if stored, exists := h.files[fileID]; exists {
				filesToZip = append(filesToZip, stored)
			}
		}
		h.mu.RUnlock()

		if len(filesToZip) == 0 {
			http.Error(w, "No files found", http.StatusNotFound)
			return
		}

		zipFilename := h.buildZipFilename(filesToZip)

		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", zipFilename))

		zipWriter := zip.NewWriter(w)
		defer zipWriter.Close()

		for _, stored := range filesToZip {
			filePath, cleanup, err := h.prepareFileWithCoverArt(stored)
			if err != nil {
				log.Printf("Handler.DownloadSelected: Failed to prepare file %s: %v, using original file", stored.Path, err)
				filePath = stored.Path
				cleanup = func() {}
			}

			if _, err := os.Stat(filePath); err != nil {
				if cleanup != nil {
					cleanup()
				}
				log.Printf("Handler.DownloadSelected: File does not exist %s: %v", filePath, err)
				continue
			}

			file, err := os.Open(filePath)
			if err != nil {
				if cleanup != nil {
					cleanup()
				}
				log.Printf("Handler.DownloadSelected: Failed to open file %s: %v", filePath, err)
				continue
			}

			fileStat, err := file.Stat()
			if err != nil {
				file.Close()
				if cleanup != nil {
					cleanup()
				}
				log.Printf("Handler.DownloadSelected: Failed to stat file %s: %v", filePath, err)
				continue
			}

			downloadFilename := h.buildDownloadFilename(stored)
			zipHeader := &zip.FileHeader{
				Name:               downloadFilename,
				Method:             zip.Deflate,
				Modified:           fileStat.ModTime(),
				UncompressedSize64: uint64(fileStat.Size()),
			}
			zipEntry, err := zipWriter.CreateHeader(zipHeader)
			if err != nil {
				file.Close()
				if cleanup != nil {
					cleanup()
				}
				log.Printf("Handler.DownloadSelected: Failed to create zip entry for %s: %v", downloadFilename, err)
				continue
			}

			_, err = io.Copy(zipEntry, file)
			file.Close()
			if cleanup != nil {
				cleanup()
			}
			if err != nil {
				log.Printf("Handler.DownloadSelected: Failed to write file %s to zip: %v", downloadFilename, err)
				continue
			}
		}

		log.Printf("Handler.DownloadSelected: ZIP file created with %d files", len(filesToZip))
	}
}

func (h *Handler) buildZipFilename(files []*storedFile) string {
	if len(files) == 0 {
		return "all-tracks.zip"
	}

	artistCount := make(map[string]int)
	albumCount := make(map[string]int)

	for _, stored := range files {
		if stored.Metadata != nil {
			if stored.Metadata.Artist != "" {
				artistCount[stored.Metadata.Artist]++
			}
			if stored.Metadata.Album != "" {
				albumCount[stored.Metadata.Album]++
			}
		}
	}

	var commonArtist string
	var commonAlbum string
	maxArtistCount := 0
	maxAlbumCount := 0

	for artist, count := range artistCount {
		if count > maxArtistCount {
			maxArtistCount = count
			commonArtist = artist
		}
	}

	for album, count := range albumCount {
		if count > maxAlbumCount {
			maxAlbumCount = count
			commonAlbum = album
		}
	}

	if commonArtist != "" && commonAlbum != "" && maxArtistCount == len(files) && maxAlbumCount == len(files) {
		filename := fmt.Sprintf("%s - %s.zip", commonArtist, commonAlbum)
		return sanitizeFilename(filename)
	}

	if commonArtist != "" && maxArtistCount == len(files) {
		filename := fmt.Sprintf("%s.zip", commonArtist)
		return sanitizeFilename(filename)
	}

	return "all-tracks.zip"
}
