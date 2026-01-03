package audio

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"
	"github.com/iamvkosarev/music-tag-editor/internal/model"
)

type AudioService struct{}

func NewAudioService() *AudioService {
	return &AudioService{}
}

func (s *AudioService) ParseFile(filePath string) (*model.FileMetadata, error) {
	result, err := parseFileWithTag(filePath)
	if err != nil {
		return result, err
	}

	fileExt := strings.ToUpper(strings.TrimPrefix(filepath.Ext(filePath), "."))
	if fileExt == "" {
		fileExt = result.Format
	}

	var duration float64
	var durationErr error

	handler := getFormatHandlerByExtension(fileExt)
	if handler != nil {
		duration, durationErr = handler.ExtractDuration(filePath)
	} else {
		file, err := openFile(filePath)
		if err == nil {
			defer file.Close()
			metadata, err := tag.ReadFrom(file)
			if err == nil {
				handler = getFormatHandlerByFileType(metadata.FileType())
				if handler != nil {
					duration, durationErr = handler.ExtractDuration(filePath)
				}
			}
		}
	}

	if durationErr == nil && duration > 0 {
		result.Duration = duration
	}

	return result, nil
}

func (s *AudioService) ParseReader(reader io.ReadSeeker, filename string, size int64) (*model.FileMetadata, error) {
	return parseReaderWithTag(reader, filename, size)
}

func (s *AudioService) UpdateTags(
	filePath string,
	title, artist, album *string,
	year, track *int,
	genre *string,
	coverArt *string,
) error {
	ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(filePath), "."))
	handler := getFormatHandlerByExtension(ext)
	if handler == nil {
		return fmt.Errorf("tag writing not yet supported for format: %s", ext)
	}
	return handler.UpdateTags(filePath, title, artist, album, year, track, genre, coverArt)
}

func (s *AudioService) ParseFLACWithAudiometa(filePath string) (*model.FileMetadata, error) {
	handler := getFLACHandler("FLAC")
	if flacHandler, ok := handler.(*flacHandler); ok {
		return flacHandler.ParseWithAudiometa(filePath)
	}
	return nil, fmt.Errorf("failed to get FLAC handler")
}

func getFormatHandlerByExtension(ext string) FormatHandler {
	ext = strings.ToUpper(ext)
	if handler := getMP3Handler(ext); handler != nil {
		return handler
	}
	if handler := getFLACHandler(ext); handler != nil {
		return handler
	}
	if handler := getOGGHandler(ext); handler != nil {
		return handler
	}
	return nil
}

func getFormatHandlerByFileType(fileType tag.FileType) FormatHandler {
	if handler := getMP3HandlerByFileType(fileType); handler != nil {
		return handler
	}
	if handler := getFLACHandlerByFileType(fileType); handler != nil {
		return handler
	}
	if handler := getOGGHandlerByFileType(fileType); handler != nil {
		return handler
	}
	return nil
}
