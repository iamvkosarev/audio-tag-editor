package audio

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/bogem/id3v2/v2"
	"github.com/dhowden/tag"
)

type mp3Handler struct{}

func newMP3Handler() *mp3Handler {
	return &mp3Handler{}
}

func (h *mp3Handler) Format() string {
	return "MP3"
}

func (h *mp3Handler) ExtractDuration(filePath string) (float64, error) {
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

	duration, err := h.extractDurationFromXing(buffer)
	if err == nil && duration > 0 {
		return duration, nil
	}

	duration, err = h.extractDurationFromFrames(file, buffer)
	if err == nil && duration > 0 {
		return duration, nil
	}

	header := buffer[0:4]
	bitrate := h.getBitrate(header)
	sampleRate := h.getSampleRate(header)

	if bitrate == 0 || sampleRate == 0 {
		return 0, fmt.Errorf("could not determine bitrate or sample rate")
	}

	duration = float64(fileSize*8) / float64(bitrate*1000)
	if duration > 0 {
		return duration, nil
	}

	return 0, fmt.Errorf("could not extract duration")
}

func (h *mp3Handler) extractDurationFromXing(buffer []byte) (float64, error) {
	for i := 0; i < len(buffer)-12; i++ {
		if string(buffer[i:i+4]) == "Xing" || string(buffer[i:i+4]) == "Info" {
			frames := uint32(buffer[i+8])<<24 | uint32(buffer[i+9])<<16 | uint32(buffer[i+10])<<8 | uint32(buffer[i+11])
			if frames > 0 {
				header := buffer[0:4]
				sampleRate := h.getSampleRate(header)
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
				sampleRate := h.getSampleRate(header)
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

func (h *mp3Handler) extractDurationFromFrames(file *os.File, buffer []byte) (float64, error) {
	stat, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("failed to get file stats for frame extraction: %w", err)
	}
	fileSize := stat.Size()

	header := buffer[0:4]
	sampleRate := h.getSampleRate(header)
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
				frameSize := h.getFrameSize(frameHeader)
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

func (h *mp3Handler) getFrameSize(header []byte) int {
	bitrate := h.getBitrate(header)
	sampleRate := h.getSampleRate(header)

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

func (h *mp3Handler) getBitrate(header []byte) int {
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

func (h *mp3Handler) getSampleRate(header []byte) int {
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

func (h *mp3Handler) UpdateTags(
	filePath string,
	title, artist, album *string,
	year, track *int,
	genre *string,
	coverArt *string,
) error {
	stat, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}
	originalModTime := stat.ModTime()

	tagFile, err := id3v2.Open(filePath, id3v2.Options{Parse: true})
	if err != nil {
		return fmt.Errorf("failed to open MP3 file: %w", err)
	}
	defer tagFile.Close()

	if title != nil {
		if *title == "" {
			tagFile.SetTitle("")
		} else {
			tagFile.SetTitle(*title)
		}
	}
	if artist != nil {
		if *artist == "" {
			tagFile.SetArtist("")
		} else {
			tagFile.SetArtist(*artist)
		}
	}
	if album != nil {
		if *album == "" {
			tagFile.SetAlbum("")
		} else {
			tagFile.SetAlbum(*album)
		}
	}
	if year != nil {
		tagFile.SetYear(fmt.Sprintf("%d", *year))
	}
	if track != nil {
		tagFile.AddTextFrame("TRCK", id3v2.EncodingUTF8, fmt.Sprintf("%d", *track))
	}
	if genre != nil {
		if *genre == "" {
			tagFile.SetGenre("")
		} else {
			tagFile.SetGenre(*genre)
		}
	}

	if coverArt != nil && *coverArt != "" {
		tagFile.DeleteFrames("APIC")
		coverData, mimeType, err := h.parseCoverArtData(*coverArt)
		if err != nil {
			return fmt.Errorf("failed to parse cover art data: %w", err)
		}
		mimeType = h.normalizeMimeType(mimeType)
		pic := id3v2.PictureFrame{
			Encoding:    id3v2.EncodingUTF8,
			MimeType:    mimeType,
			PictureType: id3v2.PTFrontCover,
			Description: "Front Cover",
			Picture:     coverData,
		}
		tagFile.AddAttachedPicture(pic)
	}

	tagFile.DeleteFrames("TXXX")

	if err := tagFile.Save(); err != nil {
		return fmt.Errorf("failed to save tags: %w", err)
	}

	if err := os.Chtimes(filePath, originalModTime, originalModTime); err != nil {
		return fmt.Errorf("failed to set modification time: %w", err)
	}

	return nil
}

func (h *mp3Handler) parseCoverArtData(dataURI string) ([]byte, string, error) {
	if !strings.HasPrefix(dataURI, "data:") {
		return nil, "", fmt.Errorf("invalid data URI format")
	}

	parts := strings.SplitN(dataURI, ",", 2)
	if len(parts) != 2 {
		return nil, "", fmt.Errorf("invalid data URI format")
	}

	header := parts[0]
	data := parts[1]

	mimeType := "image/jpeg"
	if strings.HasPrefix(header, "data:image/") {
		mimeParts := strings.Split(header, ";")
		if len(mimeParts) > 0 {
			mimePart := strings.TrimPrefix(mimeParts[0], "data:")
			if mimePart != "" {
				mimeType = mimePart
			}
		}
	}

	coverData, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode base64: %w", err)
	}

	return coverData, mimeType, nil
}

func (h *mp3Handler) normalizeMimeType(mimeType string) string {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	switch mimeType {
	case "image/jpg":
		return "image/jpeg"
	case "image/png":
		return "image/png"
	case "image/gif":
		return "image/gif"
	case "image/bmp":
		return "image/bmp"
	default:
		if strings.HasPrefix(mimeType, "image/") {
			return mimeType
		}
		return "image/jpeg"
	}
}

func getMP3Handler(ext string) FormatHandler {
	ext = strings.ToUpper(ext)
	if ext == "MP3" || ext == "MPEG" {
		return newMP3Handler()
	}
	return nil
}

func getMP3HandlerByFileType(fileType tag.FileType) FormatHandler {
	if string(fileType) == "MP3" {
		return newMP3Handler()
	}
	return nil
}
