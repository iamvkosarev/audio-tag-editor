package audio

type FormatHandler interface {
	ExtractDuration(filePath string) (float64, error)
	UpdateTags(filePath string, title, artist, album *string, year, track *int, genre *string, coverArt *string) error
	Format() string
}

