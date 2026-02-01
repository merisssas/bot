package aria2dl

import "fmt"

// FormatBytes provides a human-friendly byte size for logs and summaries.
func FormatBytes(size int64) string {
	if size == 0 {
		return "0 B"
	}

	sizes := []string{"B", "KB", "MB", "GB", "TB"}
	index := 0
	value := float64(size)
	for value >= 1024 && index < len(sizes)-1 {
		value /= 1024
		index++
	}

	return fmt.Sprintf("%.2f %s", value, sizes[index])
}
