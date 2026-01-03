package audio

import (
	"fmt"
	"os"
	"strings"

	"github.com/dhowden/tag"
	"github.com/go-flac/flacvorbis"
	"github.com/go-flac/go-flac"
	"github.com/iamvkosarev/music-tag-editor/internal/model"
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

func (h *flacHandler) UpdateTags(
	filePath string,
	title, artist, album *string,
	year, track *int,
	genre *string,
) error {
	_, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	tag, err := audiometa.OpenTag(filePath)
	if err != nil {
		return fmt.Errorf("failed to open FLAC tag: %w", err)
	}

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
	}
	if genre != nil {
		if *genre == "" {
			tag.SetGenre("")
		} else {
			tag.SetGenre(*genre)
		}
	}

	if err := os.Chmod(filePath, 0644); err != nil {
	}

	f, err := flac.ParseFile(filePath)
	if err != nil {
		if err := audiometa.SaveTag(tag); err != nil {
			if err2 := tag.Save(); err2 != nil {
				return fmt.Errorf("failed to save FLAC tags: SaveTag=%v, Save=%v", err, err2)
			}
		}
		return nil
	}

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
	}
	if track != nil {
		trackStr := fmt.Sprintf("%d", *track)
		if err := vorbisComment.Add(flacvorbis.FIELD_TRACKNUMBER, trackStr); err != nil {
		}
	}
	if genre != nil {
		if *genre != "" {
			if err := vorbisComment.Add(flacvorbis.FIELD_GENRE, *genre); err != nil {
			}
		}
	}

	marshaledBlock := vorbisComment.Marshal()
	if vorbisIndex >= 0 {
		f.Meta[vorbisIndex] = &marshaledBlock
	} else {
		f.Meta = append(f.Meta, &marshaledBlock)
	}

	if err := f.Save(filePath); err != nil {
		return fmt.Errorf("failed to save FLAC file: %w", err)
	}

	return nil
}

func (h *flacHandler) ParseWithAudiometa(filePath string) (*model.FileMetadata, error) {
	stat, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	flacTag, err := audiometa.OpenTag(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open FLAC tag: %w", err)
	}

	result := &model.FileMetadata{
		Size:   stat.Size(),
		Format: "FLAC",
		Title:  flacTag.Title(),
		Artist: flacTag.Artist(),
		Album:  flacTag.Album(),
		Genre:  flacTag.Genre(),
	}

	if result.Title == "" {
		result.Title = stat.Name()
	}

	yearStr := flacTag.Year()
	if yearStr != "" {
		var year int
		if _, err := fmt.Sscanf(yearStr, "%d", &year); err == nil {
			result.Year = year
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

	partOfSet := flacTag.PartOfSet()
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

