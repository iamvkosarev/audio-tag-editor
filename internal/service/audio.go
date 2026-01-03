package service

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
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
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
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

	result := s.extractMetadata(metadata, stat.Name(), stat.Size())
	result.Format = s.getFormat(metadata.FileType())

	fileExt := strings.ToUpper(strings.TrimPrefix(filepath.Ext(filePath), "."))
	if fileExt == "" {
		fileExt = result.Format
	}

	var duration float64
	var durationErr error

	fileTypeStr := string(metadata.FileType())
	if fileTypeStr != "" {
		duration, durationErr = s.extractDuration(filePath, metadata.FileType())
	}

	if durationErr != nil || duration <= 0 {
		if fileExt != "" && fileExt != "UNKNOWN" {
			duration, durationErr = s.extractDurationByExtension(filePath, fileExt)
		}
	}

	if durationErr == nil && duration > 0 {
		result.Duration = duration
	}

	return result, nil
}

func (s *AudioService) ParseReader(reader io.ReadSeeker, filename string, size int64) (*model.FileMetadata, error) {
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

	result := s.extractMetadata(metadata, filename, size)
	result.Format = s.getFormat(metadata.FileType())

	return result, nil
}

func (s *AudioService) extractMetadata(metadata tag.Metadata, filename string, size int64) *model.FileMetadata {
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

func (s *AudioService) getFormat(fileType tag.FileType) string {
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

func (s *AudioService) extractDuration(filePath string, fileType tag.FileType) (float64, error) {
	fileTypeStr := string(fileType)
	if fileTypeStr == "" {
		return 0, fmt.Errorf("unknown file type")
	}

	var duration float64
	var err error

	switch fileTypeStr {
	case "MP3":
		duration, err = s.extractMP3Duration(filePath)
	case "FLAC":
		duration, err = s.extractFLACDuration(filePath)
	case "OGG", "OGV", "OPUS":
		duration, err = s.extractOGGDuration(filePath)
	default:
		return 0, fmt.Errorf("unsupported format for duration extraction: %s", fileTypeStr)
	}

	if err != nil {
		return 0, fmt.Errorf("failed to extract duration for %s: %w", fileTypeStr, err)
	}

	return duration, nil
}

func (s *AudioService) extractDurationByExtension(filePath string, ext string) (float64, error) {
	var duration float64
	var err error

	switch ext {
	case "MP3", "MPEG":
		duration, err = s.extractMP3Duration(filePath)
	case "FLAC":
		duration, err = s.extractFLACDuration(filePath)
	case "OGG", "OGV", "OPUS":
		duration, err = s.extractOGGDuration(filePath)
	default:
		return 0, fmt.Errorf("unsupported extension: %s", ext)
	}

	if err != nil {
		return 0, fmt.Errorf("failed to extract duration by extension %s: %w", ext, err)
	}

	return duration, nil
}

func (s *AudioService) extractMP3Duration(filePath string) (float64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("failed to open MP3 file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("failed to get MP3 file stats: %w", err)
	}

	fileSize := stat.Size()
	if fileSize < 4 {
		return 0, fmt.Errorf("MP3 file too small")
	}

	buffer := make([]byte, 8192)
	_, err = file.ReadAt(buffer, 0)
	if err != nil {
		return 0, fmt.Errorf("failed to read MP3 file header: %w", err)
	}

	if buffer[0] != 0xFF || (buffer[1]&0xE0) != 0xE0 {
		return 0, fmt.Errorf("not a valid MP3 file")
	}

	duration, err := s.extractMP3DurationFromXing(file, buffer)
	if err == nil && duration > 0 {
		return duration, nil
	}

	duration, err = s.extractMP3DurationFromFrames(file, buffer)
	if err == nil && duration > 0 {
		return duration, nil
	}

	header := buffer[0:4]
	bitrate := s.getMP3Bitrate(header)
	sampleRate := s.getMP3SampleRate(header)

	if bitrate == 0 || sampleRate == 0 {
		return 0, fmt.Errorf("could not determine bitrate or sample rate")
	}

	duration = float64(fileSize*8) / float64(bitrate*1000)
	if duration > 0 {
		return duration, nil
	}

	return 0, fmt.Errorf("could not extract duration")
}

func (s *AudioService) extractMP3DurationFromXing(file *os.File, buffer []byte) (float64, error) {
	for i := 0; i < len(buffer)-12; i++ {
		if string(buffer[i:i+4]) == "Xing" || string(buffer[i:i+4]) == "Info" {
			frames := uint32(buffer[i+8])<<24 | uint32(buffer[i+9])<<16 | uint32(buffer[i+10])<<8 | uint32(buffer[i+11])
			if frames > 0 {
				header := buffer[0:4]
				sampleRate := s.getMP3SampleRate(header)
				if sampleRate > 0 {
					samplesPerFrame := 1152
					if (header[1]>>3)&0x03 == 3 {
						samplesPerFrame = 1152
					} else {
						samplesPerFrame = 576
					}
					duration := float64(frames) * float64(samplesPerFrame) / float64(sampleRate)
					return duration, nil
				}
			}
		}
		if string(buffer[i:i+4]) == "VBRI" {
			frames := uint32(buffer[i+14])<<24 | uint32(buffer[i+15])<<16 | uint32(buffer[i+16])<<8 | uint32(buffer[i+17])
			if frames > 0 {
				header := buffer[0:4]
				sampleRate := s.getMP3SampleRate(header)
				if sampleRate > 0 {
					samplesPerFrame := 1152
					if (header[1]>>3)&0x03 == 3 {
						samplesPerFrame = 1152
					} else {
						samplesPerFrame = 576
					}
					duration := float64(frames) * float64(samplesPerFrame) / float64(sampleRate)
					return duration, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("no Xing/VBRI header found")
}

func (s *AudioService) extractMP3DurationFromFrames(file *os.File, buffer []byte) (float64, error) {
	stat, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("failed to get file stats for frame extraction: %w", err)
	}
	fileSize := stat.Size()

	header := buffer[0:4]
	sampleRate := s.getMP3SampleRate(header)
	if sampleRate == 0 {
		return 0, fmt.Errorf("could not determine sample rate")
	}

	samplesPerFrame := 1152
	version := (buffer[1] >> 3) & 0x03
	if version != 3 {
		samplesPerFrame = 576
	}

	frameCount := 0
	pos := int64(0)
	maxPos := fileSize
	if maxPos > 512*1024 {
		maxPos = 512 * 1024
	}

	readBuffer := make([]byte, 4096)
	for pos < maxPos-4 {
		readSize := int64(4096)
		if pos+readSize > maxPos {
			readSize = maxPos - pos
		}

		n, err := file.ReadAt(readBuffer[:readSize], pos)
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, fmt.Errorf("failed to read MP3 frames: %w", err)
		}
		if n == 0 {
			break
		}

		for i := 0; i < n-4; i++ {
			if readBuffer[i] == 0xFF && (readBuffer[i+1]&0xE0) == 0xE0 {
				frameHeader := readBuffer[i : i+4]
				frameSize := s.getMP3FrameSize(frameHeader)
				if frameSize > 0 && frameSize < 1441 {
					frameCount++
					pos += int64(i) + int64(frameSize)
					break
				}
			}
		}

		if pos >= maxPos-4 {
			break
		}
	}

	if frameCount > 10 {
		avgFrameSize := float64(pos) / float64(frameCount)
		estimatedTotalFrames := float64(fileSize) / avgFrameSize
		duration := estimatedTotalFrames * float64(samplesPerFrame) / float64(sampleRate)
		if duration > 0 {
			return duration, nil
		}
	}

	return 0, fmt.Errorf("could not parse frames")
}

func (s *AudioService) getMP3FrameSize(header []byte) int {
	bitrate := s.getMP3Bitrate(header)
	sampleRate := s.getMP3SampleRate(header)

	if bitrate == 0 || sampleRate == 0 {
		return 0
	}

	padding := 0
	if (header[2]>>1)&0x01 == 1 {
		padding = 1
	}

	version := (header[1] >> 3) & 0x03
	samplesPerFrame := 1152
	if version != 3 {
		samplesPerFrame = 576
	}

	frameSize := ((samplesPerFrame / 8) * bitrate * 1000 / sampleRate) + padding
	return frameSize
}

func (s *AudioService) getMP3Bitrate(header []byte) int {
	bitrateTable := [][]int{
		{0, 0, 0, 0, 0},
		{32, 32, 32, 32, 8},
		{64, 48, 40, 48, 16},
		{96, 56, 48, 56, 24},
		{128, 64, 56, 64, 32},
		{160, 80, 64, 80, 40},
		{192, 96, 80, 96, 48},
		{224, 112, 96, 112, 56},
		{256, 128, 112, 128, 64},
		{288, 160, 128, 160, 80},
		{320, 192, 160, 192, 96},
		{352, 224, 192, 224, 112},
		{384, 256, 224, 256, 128},
		{416, 320, 256, 320, 144},
		{448, 384, 320, 384, 160},
	}

	version := (header[1] >> 3) & 0x03
	layer := (header[1] >> 1) & 0x03
	bitrateIndex := (header[2] >> 4) & 0x0F

	if version == 0 || layer != 1 || bitrateIndex == 0 || bitrateIndex == 15 {
		return 0
	}

	if version == 3 {
		idx := int(bitrateIndex)
		if idx < len(bitrateTable) {
			return bitrateTable[idx][0]
		}
	}

	return 0
}

func (s *AudioService) getMP3SampleRate(header []byte) int {
	sampleRateTable := [][]int{
		{44100, 22050, 11025},
		{48000, 24000, 12000},
		{32000, 16000, 8000},
		{0, 0, 0},
	}

	version := (header[1] >> 3) & 0x03
	sampleRateIndex := (header[2] >> 2) & 0x03

	idx := int(sampleRateIndex)
	if version == 3 {
		if idx < len(sampleRateTable) {
			return sampleRateTable[idx][0]
		}
	} else if version == 2 {
		if idx < len(sampleRateTable) {
			return sampleRateTable[idx][1]
		}
	}

	return 0
}

func (s *AudioService) extractFLACDuration(filePath string) (float64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("failed to open FLAC file: %w", err)
	}
	defer file.Close()

	header := make([]byte, 4)
	_, err = file.ReadAt(header, 0)
	if err != nil {
		return 0, fmt.Errorf("failed to read FLAC header: %w", err)
	}

	if string(header[0:4]) != "fLaC" {
		return 0, fmt.Errorf("not a valid FLAC file")
	}

	buffer := make([]byte, 32)
	_, err = file.ReadAt(buffer, 0)
	if err != nil {
		return 0, fmt.Errorf("failed to read FLAC buffer: %w", err)
	}

	if string(buffer[0:4]) != "fLaC" {
		return 0, fmt.Errorf("not a valid FLAC file")
	}

	blockHeader := buffer[4:8]
	blockType := blockHeader[0] & 0x7F
	blockSize := uint32(blockHeader[1])<<16 | uint32(blockHeader[2])<<8 | uint32(blockHeader[3])

	if blockType != 0 {
		return 0, fmt.Errorf("STREAMINFO block not found as first block")
	}

	if blockSize < 18 {
		return 0, fmt.Errorf("STREAMINFO block size too small")
	}

	var streamInfo []byte
	if len(buffer) >= 26 {
		streamInfo = buffer[8:26]
	} else {
		streamInfo = make([]byte, 18)
		_, err = file.ReadAt(streamInfo, 8)
		if err != nil {
			return 0, fmt.Errorf("failed to read FLAC stream info: %w", err)
		}
	}

	minBlockSize := uint16(streamInfo[0])<<8 | uint16(streamInfo[1])
	maxBlockSize := uint16(streamInfo[2])<<8 | uint16(streamInfo[3])

	sampleRate := uint32(streamInfo[10])<<12 | uint32(streamInfo[11])<<4 | uint32(streamInfo[12])>>4
	channels := int(((streamInfo[12] & 0x0E) >> 1) + 1)
	bitsPerSample := int(((streamInfo[12] & 0x01) << 4) | ((streamInfo[13] & 0xF0) >> 4) + 1)

	totalSamples := uint64(streamInfo[13]&0x0F)<<32 | uint64(streamInfo[14])<<24 | uint64(streamInfo[15])<<16 | uint64(streamInfo[16])<<8 | uint64(streamInfo[17])

	if sampleRate == 0 {
		return 0, fmt.Errorf("could not determine sample rate")
	}

	if totalSamples > 0 {
		duration := float64(totalSamples) / float64(sampleRate)
		if duration > 0 {
			return duration, nil
		}
	}

	stat, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("failed to get FLAC file stats: %w", err)
	}

	fileSize := stat.Size()
	if minBlockSize > 0 && maxBlockSize > 0 {
		avgBlockSize := float64(minBlockSize+maxBlockSize) / 2.0
		estimatedBlocks := float64(fileSize) / avgBlockSize
		samplesPerBlock := float64(minBlockSize)
		if maxBlockSize > 0 {
			samplesPerBlock = float64(maxBlockSize)
		}
		estimatedDuration := estimatedBlocks * samplesPerBlock / float64(sampleRate)
		if estimatedDuration > 0 {
			return estimatedDuration, nil
		}
	}

	estimatedDuration := float64(fileSize*8) / float64(int(sampleRate)*channels*bitsPerSample)
	if estimatedDuration > 0 {
		return estimatedDuration, nil
	}

	return 0, fmt.Errorf("could not extract FLAC duration")
}

func (s *AudioService) extractOGGDuration(filePath string) (float64, error) {
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
