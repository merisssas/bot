package ytdlp

import "time"

type TaskStats struct {
	StartTime     time.Time
	CompletedURLs int
	TotalURLs     int
	ActiveURL     string
	ActivePercent float64
	LastUpdate    time.Time
	LastError     string
}
