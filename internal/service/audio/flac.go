package audio

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/bogem/id3v2/v2"
	"github.com/dhowden/tag"
	"github.com/go-flac/flacpicture"
	"github.com/go-flac/flacvorbis"
	"github.com/go-flac/go-flac"
	"github.com/iamvkosarev/audio-tag-editor/internal/model"
	"github.com/iamvkosarev/audio-tag-editor/pkg/logs"
	"github.com/tallenh/audiometa"
)

type flacHandler struct{}

func newFLACHandler() *flacHandler {
	return &flacHandler{}
}

func (h *flacHandler) Format() string {
	return "FLAC"
}

func (h *flacHandler) ExtractDuration(filePath string) (float64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("failed to open FLAC file: %w", err)
	}
	defer file.Close()

	header := make([]byte, 10)
	_, err = file.ReadAt(header, 0)
	if err != nil {
		return 0, fmt.Errorf("failed to read FLAC header: %w", err)
	}

	flacStartPos := int64(0)
	if string(header[0:3]) == "ID3" {
		id3Size := int(header[6])<<21 | int(header[7])<<14 | int(header[8])<<7 | int(header[9])
		flacStartPos = int64(10 + id3Size)
	} else if string(header[0:4]) != "fLaC" {
		return 0, fmt.Errorf("not a valid FLAC file")
	}

	buffer := make([]byte, 32)
	_, err = file.ReadAt(buffer, flacStartPos)
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
		_, err = file.ReadAt(streamInfo, flacStartPos+8)
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

func (h *flacHandler) UpdateTags(
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

	onlyCoverArt := coverArt != nil && *coverArt != "" && title == nil && artist == nil && album == nil && year == nil && track == nil && genre == nil

	var audiometaUsed bool
	var existingYearFromFile int
	var existingTrackFromFile int
	var existingMetadata *model.FileMetadata
	if !onlyCoverArt && (year == nil || track == nil) {
		var parseErr error
		existingMetadata, parseErr = h.ParseWithAudiometa(filePath)
		if parseErr == nil && existingMetadata != nil {
			if year == nil && existingMetadata.Year > 0 {
				existingYearFromFile = existingMetadata.Year
			}
			if track == nil && existingMetadata.Track > 0 {
				existingTrackFromFile = existingMetadata.Track
			}
		}
	}

	if !onlyCoverArt && track == nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logs.Panic(context.Background(), "FLAC UpdateTags: audiometa panicked, falling back to direct FLAC library", r)
					audiometaUsed = false
				}
			}()

			var tagInterface interface{}
			var openErr error
			tagInterface, openErr = audiometa.OpenTag(filePath)
			if openErr != nil {
				return
			}

			audiometaUsed = true

			type AudioMetaTagReader interface {
				Year() string
			}
			var existingYearStr string
			if audioTagReader, ok := tagInterface.(AudioMetaTagReader); ok {
				existingYearStr = audioTagReader.Year()
			}
			
			if existingYearStr == "" && existingYearFromFile > 0 {
				existingYearStr = fmt.Sprintf("%d", existingYearFromFile)
			}

			type AudioMetaTagWriter interface {
				SetTitle(string)
				SetArtist(string)
				SetAlbum(string)
				SetYear(string)
				SetGenre(string)
				SetAlbumArtFromByteArray([]byte) error
				Save() error
			}
			tag := tagInterface.(AudioMetaTagWriter)

			if title != nil {
				if *title == "" {
					tag.SetTitle("")
				} else {
					tag.SetTitle(*title)
				}
			}
			if artist != nil {
				if *artist == "" {
					tag.SetArtist("")
				} else {
					tag.SetArtist(*artist)
				}
			}
			if album != nil {
				if *album == "" {
					tag.SetAlbum("")
				} else {
					tag.SetAlbum(*album)
				}
			}
			if year != nil {
				tag.SetYear(fmt.Sprintf("%d", *year))
			} else {
				if existingYearStr != "" {
					tag.SetYear(existingYearStr)
				}
			}
			if genre != nil {
				if *genre == "" {
					tag.SetGenre("")
				} else {
					tag.SetGenre(*genre)
				}
			}

			if coverArt != nil && *coverArt != "" {
				coverData, _, err := h.parseCoverArtData(*coverArt)
				if err == nil && len(coverData) > 0 {
					if err := tag.SetAlbumArtFromByteArray(coverData); err != nil {
					}
				}
			}

			if err := os.Chmod(filePath, 0644); err != nil {
			}

			if idTag, ok := tagInterface.(*audiometa.IDTag); ok {
				if err := audiometa.SaveTag(idTag); err != nil {
					type AudioMetaTagSaver interface {
						Save() error
					}
					if tagSaver, ok2 := tagInterface.(AudioMetaTagSaver); ok2 {
						if err2 := tagSaver.Save(); err2 != nil {
							audiometaUsed = false
							return
						}
					} else {
						audiometaUsed = false
						return
					}
				}
			}
			
			if (existingYearStr != "" && year == nil) || (existingYearFromFile > 0 && year == nil) {
				audiometaUsed = false
			}
			if existingTrackFromFile > 0 && track == nil {
				audiometaUsed = false
			}
		}()
	}

	if audiometaUsed {
		return nil
	}

	stat, err = os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to stat file after audiometa: %w", err)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}

	header := make([]byte, 10)
	_, err = file.ReadAt(header, 0)
	if err != nil {
		file.Close()
		return fmt.Errorf("failed to read header: %w", err)
	}

	flacStartPos := int64(0)
	var id3TagData []byte
	if string(header[0:3]) == "ID3" {
		id3Size := int(header[6])<<21 | int(header[7])<<14 | int(header[8])<<7 | int(header[9])
		flacStartPos = int64(10 + id3Size)
		id3TagData = make([]byte, flacStartPos)
		_, err = file.ReadAt(id3TagData, 0)
		if err != nil {
			file.Close()
			return fmt.Errorf("failed to read ID3 tag: %w", err)
		}
	}

	flacData := make([]byte, stat.Size()-flacStartPos)
	_, err = file.ReadAt(flacData, flacStartPos)
	file.Close()
	if err != nil {
		return fmt.Errorf("failed to read FLAC data: %w", err)
	}

	tempFlacFile, err := os.CreateTemp("", "flac-edit-*")
	if err != nil {
		return fmt.Errorf("failed to create temp FLAC file: %w", err)
	}
	tempFlacPath := tempFlacFile.Name()
	defer os.Remove(tempFlacPath)

	_, err = tempFlacFile.Write(flacData)
	tempFlacFile.Close()
	if err != nil {
		return fmt.Errorf("failed to write temp FLAC file: %w", err)
	}

	f, err := flac.ParseFile(tempFlacPath)
	if err != nil {
		return fmt.Errorf("failed to parse FLAC file: %w", err)
	}

	if !audiometaUsed && !onlyCoverArt {
		var vorbisComment *flacvorbis.MetaDataBlockVorbisComment
		var vorbisIndex int = -1

		for i, meta := range f.Meta {
			if meta.Type == flac.VorbisComment {
				vorbisComment, err = flacvorbis.ParseFromMetaDataBlock(*meta)
				if err != nil {
					continue
				}
				vorbisIndex = i
				break
			}
		}

		if vorbisComment == nil {
			vorbisComment = flacvorbis.New()
			vorbisIndex = -1
		}

		{
			newComments := []string{}
			for _, comment := range vorbisComment.Comments {
				keep := true
				upperComment := strings.ToUpper(comment)
				if title != nil && strings.HasPrefix(upperComment, "TITLE=") {
					keep = false
				}
				if artist != nil && strings.HasPrefix(upperComment, "ARTIST=") {
					keep = false
				}
				if album != nil && strings.HasPrefix(upperComment, "ALBUM=") {
					keep = false
				}
				if year != nil && strings.HasPrefix(upperComment, "DATE=") {
					keep = false
				}
				if track != nil && strings.HasPrefix(upperComment, "TRACKNUMBER=") {
					keep = false
				}
				if genre != nil && strings.HasPrefix(upperComment, "GENRE=") {
					keep = false
				}
				if strings.HasPrefix(upperComment, "DESCRIPTION=") {
					keep = false
				}
				if strings.HasPrefix(upperComment, "REPLAYGAIN_") {
					keep = false
				}
				if keep {
					newComments = append(newComments, comment)
				}
			}
			vorbisComment.Comments = newComments

			if title != nil {
				if *title != "" {
					if err := vorbisComment.Add(flacvorbis.FIELD_TITLE, *title); err != nil {
					}
				}
			}
			if artist != nil {
				if *artist != "" {
					if err := vorbisComment.Add(flacvorbis.FIELD_ARTIST, *artist); err != nil {
					}
				}
			}
			if album != nil {
				if *album != "" {
					if err := vorbisComment.Add(flacvorbis.FIELD_ALBUM, *album); err != nil {
					}
				}
			}
			if year != nil {
				yearStr := fmt.Sprintf("%d", *year)
				if err := vorbisComment.Add(flacvorbis.FIELD_DATE, yearStr); err != nil {
				}
			} else if existingYearFromFile > 0 {
				yearStr := fmt.Sprintf("%d", existingYearFromFile)
				if err := vorbisComment.Add(flacvorbis.FIELD_DATE, yearStr); err != nil {
				}
			}
			if track != nil {
				trackStr := fmt.Sprintf("%d", *track)
				if err := vorbisComment.Add(flacvorbis.FIELD_TRACKNUMBER, trackStr); err != nil {
				}
			} else if existingTrackFromFile > 0 {
				trackStr := fmt.Sprintf("%d", existingTrackFromFile)
				if err := vorbisComment.Add(flacvorbis.FIELD_TRACKNUMBER, trackStr); err != nil {
				}
			}
			if genre != nil {
				if *genre != "" {
					if err := vorbisComment.Add(flacvorbis.FIELD_GENRE, *genre); err != nil {
					}
				}
			}
		}

		marshaledBlock := vorbisComment.Marshal()
		if vorbisIndex >= 0 {
			f.Meta[vorbisIndex] = &marshaledBlock
		} else {
			f.Meta = append(f.Meta, &marshaledBlock)
		}
	}

	if coverArt != nil && *coverArt != "" {
		coverData, mimeType, err := h.parseCoverArtData(*coverArt)
		if err != nil {
			return fmt.Errorf("failed to parse cover art data: %w", err)
		}

		if len(coverData) == 0 {
			return fmt.Errorf("cover art data is empty")
		}

		pictureBlocksRemoved := false
		newMeta := make([]*flac.MetaDataBlock, 0, len(f.Meta)+1)
		for _, meta := range f.Meta {
			if meta.Type == flac.Picture {
				pictureBlocksRemoved = true
				continue
			}
			newMeta = append(newMeta, meta)
		}

		picture, err := flacpicture.NewFromImageData(
			flacpicture.PictureTypeFrontCover, "Front Cover", coverData, mimeType,
		)
		if err != nil {
			return fmt.Errorf("failed to create picture block: %w", err)
		}
		pictureBlock := picture.Marshal()
		newMeta = append(newMeta, &pictureBlock)

		f.Meta = newMeta
		_ = pictureBlocksRemoved
	}

	tempFile := filePath + ".tmp"
	if err := f.Save(tempFile); err != nil {
		return fmt.Errorf("failed to save FLAC file: %w", err)
	}

	if len(id3TagData) > 0 {
		flacFile, err := os.Open(tempFile)
		if err != nil {
			os.Remove(tempFile)
			return fmt.Errorf("failed to open temp FLAC file: %w", err)
		}
		defer flacFile.Close()

		flacStat, err := flacFile.Stat()
		if err != nil {
			os.Remove(tempFile)
			return fmt.Errorf("failed to stat temp FLAC file: %w", err)
		}

		flacContent := make([]byte, flacStat.Size())
		_, err = flacFile.ReadAt(flacContent, 0)
		flacFile.Close()
		if err != nil {
			os.Remove(tempFile)
			return fmt.Errorf("failed to read temp FLAC file: %w", err)
		}

		finalContent := append(id3TagData, flacContent...)
		if err := os.WriteFile(filePath, finalContent, 0644); err != nil {
			os.Remove(tempFile)
			return fmt.Errorf("failed to write final file: %w", err)
		}
		os.Remove(tempFile)
	} else {
		if err := os.Rename(tempFile, filePath); err != nil {
			os.Remove(tempFile)
			return fmt.Errorf("failed to rename temp file: %w", err)
		}
	}

	if coverArt != nil && *coverArt != "" {
		if err := h.addID3v2TagsForMacOS(filePath, title, artist, album, year, track, genre, coverArt); err != nil {
		}
	}

	if err := os.Chtimes(filePath, originalModTime, originalModTime); err != nil {
		return fmt.Errorf("failed to set modification time: %w", err)
	}

	return nil
}

func (h *flacHandler) addID3v2TagsForMacOS(
	filePath string,
	title, artist, album *string,
	year, track *int,
	genre *string,
	coverArt *string,
) error {
	sourceFile, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open source: %w", err)
	}
	defer sourceFile.Close()

	sourceStat, err := sourceFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}
	originalModTime := sourceStat.ModTime()

	header := make([]byte, 4)
	_, err = sourceFile.ReadAt(header, 0)
	if err != nil {
		return fmt.Errorf("failed to read header: %w", err)
	}

	flacStartPos := int64(0)
	if string(header) == "ID3" {
		existingID3v2Tag, err := id3v2.ParseReader(sourceFile, id3v2.Options{Parse: true})
		if err == nil && existingID3v2Tag != nil {
			if tagSize := existingID3v2Tag.Size(); tagSize > 0 {
				flacStartPos = int64(tagSize + 10)
			}
			existingID3v2Tag.Close()
		}
		sourceFile.Seek(0, 0)
	} else if string(header) != "fLaC" {
		return fmt.Errorf("not a FLAC file")
	}

	var existingMetadata *model.FileMetadata
	if title == nil || artist == nil || album == nil || year == nil || track == nil || genre == nil {
		existingMetadata, _ = h.ParseWithAudiometa(filePath)
	}

	id3v2Tag := id3v2.NewEmptyTag()
	id3v2Tag.SetVersion(3)

	if title != nil {
		id3v2Tag.SetTitle(*title)
	} else if existingMetadata != nil && existingMetadata.Title != "" {
		id3v2Tag.SetTitle(existingMetadata.Title)
	}

	if artist != nil {
		id3v2Tag.SetArtist(*artist)
	} else if existingMetadata != nil && existingMetadata.Artist != "" {
		id3v2Tag.SetArtist(existingMetadata.Artist)
	}

	if album != nil {
		id3v2Tag.SetAlbum(*album)
	} else if existingMetadata != nil && existingMetadata.Album != "" {
		id3v2Tag.SetAlbum(existingMetadata.Album)
	}

	if year != nil {
		id3v2Tag.SetYear(fmt.Sprintf("%d", *year))
	} else if existingMetadata != nil && existingMetadata.Year > 0 {
		id3v2Tag.SetYear(fmt.Sprintf("%d", existingMetadata.Year))
	}

	if track != nil {
		id3v2Tag.AddTextFrame("TRCK", id3v2.EncodingUTF8, fmt.Sprintf("%d", *track))
	} else if existingMetadata != nil && existingMetadata.Track > 0 {
		id3v2Tag.AddTextFrame("TRCK", id3v2.EncodingUTF8, fmt.Sprintf("%d", existingMetadata.Track))
	}

	if genre != nil {
		id3v2Tag.SetGenre(*genre)
	} else if existingMetadata != nil && existingMetadata.Genre != "" {
		id3v2Tag.SetGenre(existingMetadata.Genre)
	}

	if coverArt != nil && *coverArt != "" {
		coverData, mimeType, err := h.parseCoverArtData(*coverArt)
		if err == nil && len(coverData) > 0 {
			mimeType = h.normalizeMimeTypeForID3v2(mimeType)
			id3v2Tag.DeleteFrames("APIC")
			pic := id3v2.PictureFrame{
				Encoding:    id3v2.EncodingUTF8,
				MimeType:    mimeType,
				PictureType: id3v2.PTFrontCover,
				Description: "Front Cover",
				Picture:     coverData,
			}
			id3v2Tag.AddAttachedPicture(pic)
		}
	}

	tempFile, err := os.CreateTemp("", "flac-id3v2-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	tempFile.Close()
	defer os.Remove(tempPath)

	destFile, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create dest: %w", err)
	}
	defer destFile.Close()

	if _, err := id3v2Tag.WriteTo(destFile); err != nil {
		return fmt.Errorf("failed to write ID3v2 tag: %w", err)
	}

	if _, err := sourceFile.Seek(flacStartPos, 0); err != nil {
		return fmt.Errorf("failed to seek to FLAC start: %w", err)
	}

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return fmt.Errorf("failed to copy FLAC data: %w", err)
	}

	destFile.Close()
	sourceFile.Close()

	if err := os.Rename(tempPath, filePath); err != nil {
		return fmt.Errorf("failed to replace file: %w", err)
	}

	if err := os.Chtimes(filePath, originalModTime, originalModTime); err != nil {
		return fmt.Errorf("failed to set modification time: %w", err)
	}

	return nil
}

func (h *flacHandler) normalizeMimeTypeForID3v2(mimeType string) string {
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

func (h *flacHandler) parseCoverArtData(dataURI string) ([]byte, string, error) {
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

func (h *flacHandler) ParseWithAudiometa(filePath string) (*model.FileMetadata, error) {
	stat, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	var flacTag interface{}
	var audiometaErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				logs.Panic(context.Background(), "ParseWithAudiometa: audiometa panicked", r, slog.String("filePath", filePath))
				audiometaErr = fmt.Errorf("audiometa panic: %v", r)
			}
		}()
		flacTag, audiometaErr = audiometa.OpenTag(filePath)
	}()

	if audiometaErr != nil || flacTag == nil {
		return h.parseFLACWithDirectLibrary(filePath, stat)
	}

	type AudioMetaTag interface {
		Title() string
		Artist() string
		Album() string
		Genre() string
		Year() string
		PartOfSet() string
	}

	audioTag := flacTag.(AudioMetaTag)
	result := &model.FileMetadata{
		Size:   stat.Size(),
		Format: "FLAC",
		Title:  audioTag.Title(),
		Artist: audioTag.Artist(),
		Album:  audioTag.Album(),
		Genre:  audioTag.Genre(),
	}

	if result.Title == "" {
		result.Title = stat.Name()
	}

	yearStr := audioTag.Year()
	if yearStr != "" {
		var year int
		if _, err := fmt.Sscanf(yearStr, "%d", &year); err == nil {
			result.Year = year
		} else {
			dateParts := strings.Split(yearStr, "-")
			if len(dateParts) > 0 {
				if _, err := fmt.Sscanf(dateParts[0], "%d", &year); err == nil {
					result.Year = year
				}
			}
		}
	}

	if result.Year == 0 {
		fileForYear, err := os.Open(filePath)
		if err == nil {
			defer fileForYear.Close()
			header := make([]byte, 10)
			_, err = fileForYear.ReadAt(header, 0)
			if err == nil {
				flacStartPos := int64(0)
				if string(header[0:3]) == "ID3" {
					id3Size := int(header[6])<<21 | int(header[7])<<14 | int(header[8])<<7 | int(header[9])
					flacStartPos = int64(10 + id3Size)
				}
				flacData := make([]byte, stat.Size()-flacStartPos)
				_, err = fileForYear.ReadAt(flacData, flacStartPos)
				if err == nil {
					flacReader := bytes.NewReader(flacData)
					f, err := flac.ParseMetadata(flacReader)
					if err == nil {
						for _, meta := range f.Meta {
							if meta.Type == flac.VorbisComment {
								vorbisComment, err := flacvorbis.ParseFromMetaDataBlock(*meta)
								if err == nil {
									for _, comment := range vorbisComment.Comments {
										upperComment := strings.ToUpper(comment)
										if strings.HasPrefix(upperComment, "DATE=") {
											parts := strings.SplitN(comment, "=", 2)
											if len(parts) == 2 {
												dateStr := parts[1]
												if dateStr != "" {
													var year int
													if _, err := fmt.Sscanf(dateStr, "%d", &year); err == nil {
														result.Year = year
														break
													} else {
														dateParts := strings.Split(dateStr, "-")
														if len(dateParts) > 0 {
															if _, err := fmt.Sscanf(
																dateParts[0], "%d", &year,
															); err == nil {
																result.Year = year
																break
															}
														}
													}
												}
											}
										}
									}
									break
								}
							}
						}
					}
				}
			}
		}
	}

	fileForTrack, err := os.Open(filePath)
	if err == nil {
		defer fileForTrack.Close()
		fileForTrack.Seek(0, 0)
		tagMetadata, err := tag.ReadFrom(fileForTrack)
		if err == nil {
			trackNum, _ := tagMetadata.Track()
			result.Track = trackNum
		}
	}

	partOfSet := audioTag.PartOfSet()
	if partOfSet != "" {
		var disc int
		if _, err := fmt.Sscanf(partOfSet, "%d", &disc); err == nil {
			result.Disc = disc
		} else {
			parts := strings.Split(partOfSet, "/")
			if len(parts) > 0 {
				if _, err := fmt.Sscanf(parts[0], "%d", &disc); err == nil {
					result.Disc = disc
				}
			}
		}
	}

	duration, err := h.ExtractDuration(filePath)
	if err == nil && duration > 0 {
		result.Duration = duration
	}

	f, err := flac.ParseFile(filePath)
	if err == nil {
		for _, meta := range f.Meta {
			if meta.Type == flac.Picture {
				picture, err := flacpicture.ParseFromMetaDataBlock(*meta)
				if err == nil {
					if len(picture.ImageData) > 0 {
						mimeType := picture.MIME
						if mimeType == "" {
							mimeType = "image/jpeg"
						}
						base64Data := base64.StdEncoding.EncodeToString(picture.ImageData)
						result.CoverArt = fmt.Sprintf("data:%s;base64,%s", mimeType, base64Data)
						break
					}
				}
			}
		}
	}

	return result, nil
}

func (h *flacHandler) parseFLACWithDirectLibrary(filePath string, stat os.FileInfo) (*model.FileMetadata, error) {
	result := &model.FileMetadata{
		Size:   stat.Size(),
		Format: "FLAC",
		Title:  stat.Name(),
	}

	file, err := os.Open(filePath)
	if err != nil {
		return result, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	header := make([]byte, 10)
	_, err = file.ReadAt(header, 0)
	if err != nil {
		return result, fmt.Errorf("failed to read header: %w", err)
	}

	flacStartPos := int64(0)
	if string(header[0:3]) == "ID3" {
		id3Size := int(header[6])<<21 | int(header[7])<<14 | int(header[8])<<7 | int(header[9])
		flacStartPos = int64(10 + id3Size)
	}

	flacData := make([]byte, stat.Size()-flacStartPos)
	_, err = file.ReadAt(flacData, flacStartPos)
	if err != nil {
		return result, fmt.Errorf("failed to read FLAC data: %w", err)
	}

	flacReader := bytes.NewReader(flacData)
	f, err := flac.ParseMetadata(flacReader)
	if err != nil {
		return result, fmt.Errorf("failed to parse FLAC file: %w", err)
	}

	var vorbisComment *flacvorbis.MetaDataBlockVorbisComment
	for _, meta := range f.Meta {
		if meta.Type == flac.VorbisComment {
			vorbisComment, err = flacvorbis.ParseFromMetaDataBlock(*meta)
			if err == nil {
				break
			}
		}
	}

	if vorbisComment != nil {
		for _, comment := range vorbisComment.Comments {
			upperComment := strings.ToUpper(comment)
			if strings.HasPrefix(upperComment, "TITLE=") {
				parts := strings.SplitN(comment, "=", 2)
				if len(parts) == 2 {
					result.Title = parts[1]
				}
				if result.Title == "" {
					result.Title = stat.Name()
				}
			} else if strings.HasPrefix(upperComment, "ARTIST=") {
				parts := strings.SplitN(comment, "=", 2)
				if len(parts) == 2 {
					result.Artist = parts[1]
				}
			} else if strings.HasPrefix(upperComment, "ALBUM=") {
				parts := strings.SplitN(comment, "=", 2)
				if len(parts) == 2 {
					result.Album = parts[1]
				}
			} else if strings.HasPrefix(upperComment, "DATE=") {
				parts := strings.SplitN(comment, "=", 2)
				if len(parts) == 2 {
					yearStr := parts[1]
					if yearStr != "" {
						var year int
						if _, err := fmt.Sscanf(yearStr, "%d", &year); err == nil {
							result.Year = year
						} else {
							dateParts := strings.Split(yearStr, "-")
							if len(dateParts) > 0 {
								if _, err := fmt.Sscanf(dateParts[0], "%d", &year); err == nil {
									result.Year = year
								}
							}
						}
					}
				}
			} else if strings.HasPrefix(upperComment, "TRACKNUMBER=") {
				parts := strings.SplitN(comment, "=", 2)
				if len(parts) == 2 {
					trackStr := parts[1]
					if trackStr != "" {
						var track int
						if _, err := fmt.Sscanf(trackStr, "%d", &track); err == nil {
							result.Track = track
						}
					}
				}
			} else if strings.HasPrefix(upperComment, "GENRE=") {
				parts := strings.SplitN(comment, "=", 2)
				if len(parts) == 2 {
					result.Genre = parts[1]
				}
			} else if strings.HasPrefix(upperComment, "DISCNUMBER=") {
				parts := strings.SplitN(comment, "=", 2)
				if len(parts) == 2 {
					discStr := parts[1]
					if discStr != "" {
						var disc int
						if _, err := fmt.Sscanf(discStr, "%d", &disc); err == nil {
							result.Disc = disc
						}
					}
				}
			}
		}
	}

	for _, meta := range f.Meta {
		if meta.Type == flac.Picture {
			picture, err := flacpicture.ParseFromMetaDataBlock(*meta)
			if err == nil {
				if len(picture.ImageData) > 0 {
					mimeType := picture.MIME
					if mimeType == "" {
						mimeType = "image/jpeg"
					}
					base64Data := base64.StdEncoding.EncodeToString(picture.ImageData)
					result.CoverArt = fmt.Sprintf("data:%s;base64,%s", mimeType, base64Data)
					break
				}
			}
		}
	}

	duration, err := h.ExtractDuration(filePath)
	if err == nil && duration > 0 {
		result.Duration = duration
	}

	return result, nil
}

func getFLACHandler(ext string) FormatHandler {
	ext = strings.ToUpper(ext)
	if ext == "FLAC" {
		return newFLACHandler()
	}
	return nil
}

func getFLACHandlerByFileType(fileType tag.FileType) FormatHandler {
	if string(fileType) == "FLAC" {
		return newFLACHandler()
	}
	return nil
}
