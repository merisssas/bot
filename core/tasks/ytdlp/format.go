package ytdlp

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	ytdlp "github.com/lrstanley/go-ytdlp"
)

// FormatChoice represents a smart selection option for the user.
type FormatChoice struct {
	ID            string   // Unique ID (e.g., "res:1080")
	Label         string   // Human readable: "1080p60 HDR (MP4)"
	Resolution    string   // "1920x1080"
	Extension     string   // "mp4", "mkv", "mp3"
	Flags         []string // The precise yt-dlp flags to get this
	EstimatedSize int64    // Calculated size in bytes
	IsAudioOnly   bool
	Height        int
	Extras        []string
}

// FetchFormatChoices analyzes the video URL and returns the best available options using heuristics.
func FetchFormatChoices(ctx context.Context, url string, cookiePath string) ([]FormatChoice, error) {
	// 1. Fetch Metadata (Fast & Robust)
	logger := log.FromContext(ctx)
	cmd := ytdlp.New().
		SetCancelMaxWait(10 * time.Second). // Slightly more tolerant for slow connections
		SetSeparateProcessGroup(true).
		DumpSingleJSON().
		NoPlaylist() // Ensure we only analyze a single video

	args := defaultYtdlpArgs()
	args = appendProxyArgs(ctx, logger, args, []string{url}, nil)
	if cookiePath != "" {
		args = append(args, "--cookies", cookiePath)
	}
	args = append(args, url)

	result, err := cmd.Run(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("metadata fetch failed: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("metadata fetch exited with code %d: %s", result.ExitCode, result.Stderr)
	}

	info, err := result.GetExtractedInfo()
	if err != nil {
		return nil, fmt.Errorf("metadata parse error: %w", err)
	}
	if len(info) == 0 || info[0] == nil {
		return nil, fmt.Errorf("empty metadata received")
	}

	extracted := info[0]
	formats := extracted.Formats
	duration := 0.0
	if extracted.Duration != nil {
		duration = *extracted.Duration
	}

	if len(formats) == 0 {
		return nil, fmt.Errorf("no formats found")
	}

	// 2. Intelligence Phase: Analyze & Group Formats
	return generateSmartChoices(formats, duration), nil
}

// generateSmartChoices filters and groups formats into user-friendly tiers.
func generateSmartChoices(formats []*ytdlp.ExtractedFormat, duration float64) []FormatChoice {
	var choices []FormatChoice

	// Map to track if we already added a specific resolution
	seenRes := make(map[int]bool)

	// Target Resolutions (Descending quality)
	// We look for 8K, 4K, 2K, 1080p, 720p, 480p, 360p
	targets := []int{4320, 2160, 1440, 1080, 720, 480, 360}

	// 3. Best Audio Calculation
	bestAudio := pickBestAudioFormat(formats)
	audioSize := calculateAccurateSize(bestAudio, duration)

	// 4. Video Processing Loop
	for _, targetHeight := range targets {
		bestVideo := pickBestVideoForHeight(formats, targetHeight)

		// Skip if this resolution isn't available
		if bestVideo == nil {
			continue
		}

		// Avoid duplicates (e.g. multiple 1080p variants)
		actualHeight := formatHeight(bestVideo)

		// Tolerance: some videos are 1082p or 1079p
		// Normalize to the nearest target for grouping
		normalizedHeight := normalizeHeight(actualHeight)

		if seenRes[normalizedHeight] {
			continue
		}

		videoSize := calculateAccurateSize(bestVideo, duration)
		totalSize := videoSize + audioSize

		// Construct Label Components
		var extras []string
		if is60FPS(bestVideo) {
			extras = append(extras, "60fps")
		}
		if isHDR(bestVideo) {
			extras = append(extras, "HDR")
		}
		extrasStr := ""
		if len(extras) > 0 {
			extrasStr = " " + strings.Join(extras, " ")
		}

		choices = append(choices, FormatChoice{
			ID:            fmt.Sprintf("video-%d", normalizedHeight),
			Label:         fmt.Sprintf("%dp%s (MP4)", normalizedHeight, extrasStr),
			Resolution:    fmt.Sprintf("%dx%d", formatWidth(bestVideo), formatHeight(bestVideo)),
			Extension:     "mp4",
			EstimatedSize: totalSize,
			IsAudioOnly:   false,
			Height:        normalizedHeight,
			Extras:        extras,
			// The Magic String: Force MP4 container, best video <= target height, best audio
			Flags: []string{
				"--format",
				fmt.Sprintf("bv*[height<=%d][ext=mp4]+ba[ext=m4a]/b[height<=%d][ext=mp4]/bv*[height<=%d]+ba/b[height<=%d]",
					actualHeight, actualHeight, actualHeight, actualHeight),
				"--merge-output-format", "mp4",
			},
		})

		seenRes[normalizedHeight] = true
	}

	// 5. Audio Only Option
	choices = append(choices, FormatChoice{
		ID:            "audio-best",
		Label:         "Audio Only (Best Quality)",
		Resolution:    "Audio",
		Extension:     "mp3",
		EstimatedSize: audioSize,
		IsAudioOnly:   true,
		Flags:         []string{"--format", "bestaudio", "--extract-audio", "--audio-format", "mp3"},
	})

	return choices
}

// --- Intelligence Helper Functions ---

// calculateAccurateSize uses heuristic logic: FileSize > FileSizeApprox > (Bitrate * Duration)
func calculateAccurateSize(f *ytdlp.ExtractedFormat, duration float64) int64 {
	if f == nil {
		return 0
	}
	// Priority 1: Exact Size
	if f.FileSize != nil && *f.FileSize > 0 {
		return int64(*f.FileSize)
	}
	// Priority 2: Approx Size
	if f.FileSizeApprox != nil && *f.FileSizeApprox > 0 {
		return int64(*f.FileSizeApprox)
	}
	// Priority 3: Calculation from Bitrate (bulletproof fallback)
	bitrate := formatBitrate(f)
	if bitrate > 0 && duration > 0 {
		// bitrate is usually in kbps or kilobits.
		// (kbps * 1000 * duration) / 8 = bytes
		// Note: yt-dlp TBR is usually in kbit/s
		sizeBits := (bitrate * 1000) * duration
		return int64(sizeBits / 8)
	}
	return 0
}

func pickBestVideoForHeight(formats []*ytdlp.ExtractedFormat, targetHeight int) *ytdlp.ExtractedFormat {
	var candidates []*ytdlp.ExtractedFormat

	for _, f := range formats {
		if f == nil || !isVideoFormat(f) {
			continue
		}
		h := formatHeight(f)
		// Allow small margin of error (e.g. 1080p might be 1072 or 1088)
		if h > targetHeight+10 || h < targetHeight-10 {
			continue
		}
		candidates = append(candidates, f)
	}

	if len(candidates) == 0 {
		return nil
	}

	// Sort candidates: Bitrate DESC, then Framerate DESC
	sort.Slice(candidates, func(i, j int) bool {
		// Prioritize Bitrate/Quality
		br1 := formatBitrate(candidates[i])
		br2 := formatBitrate(candidates[j])
		if math.Abs(br1-br2) > 100 { // If diff > 100kbps, bigger is better
			return br1 > br2
		}
		// If bitrate similar, prioritize Framerate (60fps > 30fps)
		fr1 := formatFPS(candidates[i])
		fr2 := formatFPS(candidates[j])
		return fr1 > fr2
	})

	return candidates[0]
}

func pickBestAudioFormat(formats []*ytdlp.ExtractedFormat) *ytdlp.ExtractedFormat {
	var best *ytdlp.ExtractedFormat
	for _, f := range formats {
		if f == nil || !isAudioFormat(f) || isVideoFormat(f) {
			continue
		}
		if best == nil {
			best = f
			continue
		}
		// Logic: Bitrate wins, then Size
		if formatBitrate(f) > formatBitrate(best) {
			best = f
		}
	}
	return best
}

// --- Low Level Parsing ---

func isVideoFormat(f *ytdlp.ExtractedFormat) bool {
	return f.VCodec != nil && *f.VCodec != "none"
}

func isAudioFormat(f *ytdlp.ExtractedFormat) bool {
	return f.ACodec != nil && *f.ACodec != "none"
}

func formatHeight(f *ytdlp.ExtractedFormat) int {
	if f == nil || f.Height == nil {
		return 0
	}
	return int(*f.Height)
}

func formatWidth(f *ytdlp.ExtractedFormat) int {
	if f == nil || f.Width == nil {
		return 0
	}
	return int(*f.Width)
}

func formatFPS(f *ytdlp.ExtractedFormat) float64 {
	if f == nil || f.FPS == nil {
		return 0
	}
	return *f.FPS
}

func is60FPS(f *ytdlp.ExtractedFormat) bool {
	return formatFPS(f) > 50
}

func isHDR(f *ytdlp.ExtractedFormat) bool {
	if f == nil {
		return false
	}
	hint := strings.ToLower(formatNote(f) + " " + formatDescription(f))
	if strings.Contains(hint, "sdr") {
		return false
	}
	return strings.Contains(hint, "hdr")
}

func formatBitrate(f *ytdlp.ExtractedFormat) float64 {
	if f == nil {
		return 0
	}
	// TBR (Total Bitrate) is best
	if f.TBR != nil {
		return *f.TBR
	}
	// Fallback to VBR/ABR
	if f.VBR != nil {
		return *f.VBR
	}
	if f.ABR != nil {
		return *f.ABR
	}
	return 0
}

func formatNote(f *ytdlp.ExtractedFormat) string {
	if f == nil || f.FormatNote == nil {
		return ""
	}
	return *f.FormatNote
}

func formatDescription(f *ytdlp.ExtractedFormat) string {
	if f == nil || f.Format == nil {
		return ""
	}
	return *f.Format
}

func normalizeHeight(h int) int {
	// Normalize weird heights to standard buckets for ID generation
	switch {
	case h >= 4000:
		return 4320
	case h >= 2000:
		return 2160
	case h >= 1400:
		return 1440
	case h >= 1000:
		return 1080
	case h >= 700:
		return 720
	case h >= 400:
		return 480
	default:
		return 360
	}
}

func defaultYtdlpArgs() []string {
	return []string{
		"--user-agent",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
		"--add-header", "Accept-Language:en-US,en;q=0.9",
		"--no-warnings",
	}
}
