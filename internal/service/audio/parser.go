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

	contentFormat, contentErr := detectFormatFromContent(file)
	if contentFormat != "" {
		log.Printf("parseFileWithTag: Content format detected: %s for file: %s", contentFormat, filePath)
	}
	if contentErr != nil {
		log.Printf("parseFileWithTag: Content format detection error: %v for file: %s", contentErr, filePath)
	}
	
	detectedFormat := contentFormat
	if detectedFormat == "" {
		detectedFormat = detectFormatFromFilePath(filePath)
		log.Printf("parseFileWithTag: Re-detected format from file path: %s for file: %s", detectedFormat, filePath)
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
				log.Printf("parseFileWithTag: Successfully parsed FLAC file with audiometa for file: %s", filePath)
				return flacResult, nil
			}
			log.Printf("parseFileWithTag: Failed to parse FLAC with audiometa: %v, falling back to tag library for file: %s", flacErr, filePath)
		}
	}
	
	file.Seek(0, 0)
	metadata, err := tag.ReadFrom(file)
	if err != nil {
		log.Printf("parseFileWithTag: tag.ReadFrom failed, using format: %s for file: %s", detectedFormat, filePath)
		return &model.FileMetadata{
			Title:    stat.Name(),
			Duration: 0,
			Size:     stat.Size(),
			Format:   detectedFormat,
		}, fmt.Errorf("failed to read tags from file: %w", err)
	}

	result := extractMetadata(metadata, stat.Name(), stat.Size())
	tagFormat := getFormat(metadata.FileType())
	log.Printf("parseFileWithTag: Tag library format: %s, Content format: %s for file: %s", tagFormat, contentFormat, filePath)
	
	if detectedFormat != "" && detectedFormat != "UNKNOWN" {
		log.Printf("parseFileWithTag: Using detected format: %s for file: %s", detectedFormat, filePath)
		result.Format = detectedFormat
		return result, nil
	}
	
	ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(stat.Name()), "."))
	if ext != "" {
		log.Printf("parseFileWithTag: Using extension format: %s for file: %s", ext, filePath)
		result.Format = ext
		return result, nil
	}
	
	if tagFormat != "UNKNOWN" && tagFormat != "" {
		log.Printf("parseFileWithTag: Using tag library format: %s for file: %s", tagFormat, filePath)
		result.Format = tagFormat
		return result, nil
	}
	
	log.Printf("parseFileWithTag: Using UNKNOWN format for file: %s", filePath)
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
		log.Printf("detectFormatFromContent: Found ID3 tag, size: %d, FLAC should be at offset: %d", id3Size, flacOffset)
		
		if flacOffset > n {
			flacHeader := make([]byte, 4)
			readN, readErr := file.ReadAt(flacHeader, int64(flacOffset))
			if readErr == nil && readN == 4 {
				if string(flacHeader) == "fLaC" {
					log.Printf("detectFormatFromContent: Found FLAC signature after large ID3 tag at offset %d", flacOffset)
					return "FLAC", nil
				} else {
					log.Printf("detectFormatFromContent: No FLAC signature at offset %d, found: %v", flacOffset, flacHeader)
				}
			} else {
				log.Printf("detectFormatFromContent: Failed to read at offset %d: %v", flacOffset, readErr)
			}
		} else {
			if flacOffset+4 <= n && string(header[flacOffset:flacOffset+4]) == "fLaC" {
				log.Printf("detectFormatFromContent: Found FLAC signature after ID3 tag at offset %d (within buffer)", flacOffset)
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
			log.Printf("detectFormatFromHeader: Found FLAC signature at offset %d", i)
			return "FLAC", nil
		}
		if string(header[i:i+4]) == "OggS" {
			return "OGG", nil
		}
	}

	if readLen >= 10 && string(header[0:3]) == "ID3" {
		id3Size := int(header[6])<<21 | int(header[7])<<14 | int(header[8])<<7 | int(header[9])
		flacOffset := 10 + id3Size
		log.Printf("detectFormatFromHeader: Found ID3 tag, size: %d, checking for FLAC at offset %d", id3Size, flacOffset)
		
		if flacOffset+4 <= readLen {
			if string(header[flacOffset:flacOffset+4]) == "fLaC" {
				log.Printf("detectFormatFromHeader: Found FLAC signature after ID3 tag at offset %d", flacOffset)
				return "FLAC", nil
			}
		} else {
			log.Printf("detectFormatFromHeader: ID3 tag size %d exceeds read buffer %d, need to read more", id3Size, readLen)
		}
		
		log.Printf("detectFormatFromHeader: Found ID3 at start, but no FLAC signature found in %d bytes", readLen)
		return "MP3", nil
	}

	if readLen >= 2 && header[0] == 0xFF && (header[1]&0xE0) == 0xE0 {
		return "MP3", nil
	}

	log.Printf("detectFormatFromHeader: No format signature found in %d bytes", readLen)
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

