package audio

import (
	"fmt"
	"os"
	"strings"

	"github.com/dhowden/tag"
)

type oggHandler struct{}

func newOGGHandler() *oggHandler {
	return &oggHandler{}
}

func (h *oggHandler) Format() string {
	return "OGG"
}

func (h *oggHandler) ExtractDuration(filePath string) (float64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("failed to open OGG file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("failed to get OGG file stats: %w", err)
	}

	buffer := make([]byte, 8192)
	readPos := stat.Size() - 8192
	if readPos < 0 {
		readPos = 0
	}
	_, err = file.ReadAt(buffer, readPos)
	if err != nil {
		return 0, fmt.Errorf("failed to read OGG file tail: %w", err)
	}

	for i := len(buffer) - 5; i >= 0; i-- {
		if string(buffer[i:i+5]) == "vorbis" {
			if i+12 < len(buffer) {
				sampleRate := uint32(buffer[i+11])<<24 | uint32(buffer[i+10])<<16 | uint32(buffer[i+9])<<8 | uint32(buffer[i+8])
				if sampleRate > 0 {
					estimatedDuration := float64(stat.Size()*8) / float64(sampleRate*16)
					return estimatedDuration, nil
				}
			}
		}
	}

	return 0, fmt.Errorf("could not determine OGG duration")
}

func (h *oggHandler) UpdateTags(
	filePath string,
	title, artist, album *string,
	year, track *int,
	genre *string,
	coverArt *string,
) error {
	return fmt.Errorf("tag writing not yet supported for format: OGG")
}

func getOGGHandler(ext string) FormatHandler {
	ext = strings.ToUpper(ext)
	if ext == "OGG" || ext == "OGV" || ext == "OPUS" {
		return newOGGHandler()
	}
	return nil
}

func getOGGHandlerByFileType(fileType tag.FileType) FormatHandler {
	fileTypeStr := string(fileType)
	if fileTypeStr == "OGG" || fileTypeStr == "OGV" || fileTypeStr == "OPUS" {
		return newOGGHandler()
	}
	return nil
}

