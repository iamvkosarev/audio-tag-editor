package audio

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"
	"github.com/iamvkosarev/music-tag-editor/internal/model"
)

func extractMetadata(metadata tag.Metadata, filename string, size int64) *model.FileMetadata {
	result := &model.FileMetadata{
		Size: size,
	}

	if metadata == nil {
		result.Title = filename
		return result
	}

	result.Title = metadata.Title()
	if result.Title == "" {
		result.Title = filename
	}

	result.Artist = metadata.Artist()
	result.Album = metadata.Album()
	result.Year = metadata.Year()
	result.Genre = metadata.Genre()

	track, _ := metadata.Track()
	result.Track = track

	disc, _ := metadata.Disc()
	result.Disc = disc

	picture := metadata.Picture()
	if picture != nil && len(picture.Data) > 0 {
		mimeType := picture.MIMEType
		if mimeType == "" {
			mimeType = "image/jpeg"
		}
		base64Data := base64.StdEncoding.EncodeToString(picture.Data)
		result.CoverArt = fmt.Sprintf("data:%s;base64,%s", mimeType, base64Data)
	}

	return result
}

func getFormat(fileType tag.FileType) string {
	fileTypeStr := string(fileType)
	switch fileTypeStr {
	case "MP3":
		return "MP3"
	case "FLAC":
		return "FLAC"
	case "OGG", "OGV", "OPUS":
		return "OGG"
	default:
		if fileTypeStr == "" {
			return "UNKNOWN"
		}
		return strings.ToUpper(fileTypeStr)
	}
}

func parseFileWithTag(filePath string) (*model.FileMetadata, error) {
	file, err := openFile(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to get file stats: %w", err)
	}

	contentFormat, _ := detectFormatFromContent(file)
	
	detectedFormat := contentFormat
	if detectedFormat == "" {
		detectedFormat = detectFormatFromFilePath(filePath)
	}
	if detectedFormat == "" {
		detectedFormat = strings.ToUpper(strings.TrimPrefix(filepath.Ext(stat.Name()), "."))
	}
	
	if detectedFormat == "FLAC" {
		handler := getFLACHandler("FLAC")
		if flacHandler, ok := handler.(*flacHandler); ok {
			var flacResult *model.FileMetadata
			var flacErr error
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("parseFileWithTag: audiometa panicked: %v, falling back to tag library for file: %s", r, filePath)
						flacErr = fmt.Errorf("audiometa panic: %v", r)
					}
				}()
				flacResult, flacErr = flacHandler.ParseWithAudiometa(filePath)
			}()
			if flacErr == nil && flacResult != nil {
				return flacResult, nil
			}
		}
	}
	
	file.Seek(0, 0)
	metadata, err := tag.ReadFrom(file)
	if err != nil {
		return &model.FileMetadata{
			Title:    stat.Name(),
			Duration: 0,
			Size:     stat.Size(),
			Format:   detectedFormat,
		}, fmt.Errorf("failed to read tags from file: %w", err)
	}

	result := extractMetadata(metadata, stat.Name(), stat.Size())
	tagFormat := getFormat(metadata.FileType())
	
	if detectedFormat != "" && detectedFormat != "UNKNOWN" {
		result.Format = detectedFormat
		return result, nil
	}
	
	ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(stat.Name()), "."))
	if ext != "" {
		result.Format = ext
		return result, nil
	}
	
	if tagFormat != "UNKNOWN" && tagFormat != "" {
		result.Format = tagFormat
		return result, nil
	}
	
	result.Format = "UNKNOWN"
	return result, nil
}

func parseReaderWithTag(reader io.ReadSeeker, filename string, size int64) (*model.FileMetadata, error) {
	contentFormat, _ := detectFormatFromReader(reader)
	
	reader.Seek(0, 0)
	metadata, err := tag.ReadFrom(reader)
	if err != nil {
		detectedFormat := contentFormat
		if detectedFormat == "" {
			detectedFormat = strings.ToUpper(strings.TrimPrefix(filepath.Ext(filename), "."))
		}
		return &model.FileMetadata{
			Title:    filename,
			Duration: 0,
			Size:     size,
			Format:   detectedFormat,
		}, fmt.Errorf("failed to read tags from reader: %w", err)
	}

	result := extractMetadata(metadata, filename, size)
	
	detectedFormat := contentFormat
	if detectedFormat == "" {
		tagFormat := getFormat(metadata.FileType())
		if tagFormat != "UNKNOWN" && tagFormat != "" {
			detectedFormat = tagFormat
		}
	}
	
	if detectedFormat == "" {
		detectedFormat = strings.ToUpper(strings.TrimPrefix(filepath.Ext(filename), "."))
	}
	
	result.Format = detectedFormat

	return result, nil
}

func openFile(filePath string) (*os.File, error) {
	return os.Open(filePath)
}

func detectFormatFromContent(file *os.File) (string, error) {
	header := make([]byte, 4096)
	n, err := file.ReadAt(header, 0)
	if err != nil && n < 4 {
		return "", fmt.Errorf("failed to read file header: %w", err)
	}
	if n < 4 {
		return "", fmt.Errorf("file too small")
	}

	if n >= 10 && string(header[0:3]) == "ID3" {
		id3Size := int(header[6])<<21 | int(header[7])<<14 | int(header[8])<<7 | int(header[9])
		flacOffset := 10 + id3Size
		
		if flacOffset > n {
			flacHeader := make([]byte, 4)
			readN, readErr := file.ReadAt(flacHeader, int64(flacOffset))
			if readErr == nil && readN == 4 {
				if string(flacHeader) == "fLaC" {
					return "FLAC", nil
				}
			}
		} else {
			if flacOffset+4 <= n && string(header[flacOffset:flacOffset+4]) == "fLaC" {
				return "FLAC", nil
			}
		}
	}

	format, err := detectFormatFromHeader(header, n)
	return format, err
}

func detectFormatFromReader(reader io.ReadSeeker) (string, error) {
	reader.Seek(0, 0)
	header := make([]byte, 4096)
	n, err := reader.Read(header)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("failed to read file header: %w", err)
	}
	if n < 4 {
		return "", fmt.Errorf("file too small")
	}
	return detectFormatFromHeader(header, n)
}

func detectFormatFromHeader(header []byte, readLen int) (string, error) {
	if readLen < 4 {
		return "", fmt.Errorf("header too short")
	}

	for i := 0; i <= readLen-4; i++ {
		if string(header[i:i+4]) == "fLaC" {
			return "FLAC", nil
		}
		if string(header[i:i+4]) == "OggS" {
			return "OGG", nil
		}
	}

	if readLen >= 10 && string(header[0:3]) == "ID3" {
		id3Size := int(header[6])<<21 | int(header[7])<<14 | int(header[8])<<7 | int(header[9])
		flacOffset := 10 + id3Size
		
		if flacOffset+4 <= readLen {
			if string(header[flacOffset:flacOffset+4]) == "fLaC" {
				return "FLAC", nil
			}
		}
		
		return "MP3", nil
	}

	if readLen >= 2 && header[0] == 0xFF && (header[1]&0xE0) == 0xE0 {
		return "MP3", nil
	}

	return "", fmt.Errorf("unknown file format")
}

func detectFormatFromFilePath(filePath string) string {
	file, err := os.Open(filePath)
	if err != nil {
		return ""
	}
	defer file.Close()

	format, err := detectFormatFromContent(file)
	if err == nil {
		return format
	}

	ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(filePath), "."))
	return ext
}

