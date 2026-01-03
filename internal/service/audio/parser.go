package audio

import (
	"encoding/base64"
	"fmt"
	"io"
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

	metadata, err := tag.ReadFrom(file)
	if err != nil {
		ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(stat.Name()), "."))
		return &model.FileMetadata{
			Title:    stat.Name(),
			Duration: 0,
			Size:     stat.Size(),
			Format:   ext,
		}, fmt.Errorf("failed to read tags from file: %w", err)
	}

	result := extractMetadata(metadata, stat.Name(), stat.Size())
	result.Format = getFormat(metadata.FileType())

	return result, nil
}

func parseReaderWithTag(reader io.ReadSeeker, filename string, size int64) (*model.FileMetadata, error) {
	metadata, err := tag.ReadFrom(reader)
	if err != nil {
		ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(filename), "."))
		return &model.FileMetadata{
			Title:    filename,
			Duration: 0,
			Size:     size,
			Format:   ext,
		}, fmt.Errorf("failed to read tags from reader: %w", err)
	}

	result := extractMetadata(metadata, filename, size)
	result.Format = getFormat(metadata.FileType())

	return result, nil
}

func openFile(filePath string) (*os.File, error) {
	return os.Open(filePath)
}

