package model

type FileMetadata struct {
	ID       string  `json:"id"`
	CoverArt string  `json:"coverArt"`
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Album    string  `json:"album"`
	Year     int     `json:"year"`
	Genre    string  `json:"genre"`
	Track    int     `json:"track"`
	Disc     int     `json:"disc"`
	Duration float64 `json:"duration"`
	Size     int64   `json:"size"`
	Format   string  `json:"format"`
}
