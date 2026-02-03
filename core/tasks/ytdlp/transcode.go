package ytdlp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/log"
)

func (t *Task) transcodeIfNeeded(ctx context.Context, logger *log.Logger, filePath string) (string, func(), error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == ".mp4" || ext == ".mkv" {
		return filePath, nil, nil
	}

	outputPath := strings.TrimSuffix(filePath, filepath.Ext(filePath)) + ".mp4"
	logger.Infof("🎞️ Transcoding %s -> %s", filepath.Base(filePath), filepath.Base(outputPath))

	cmd := exec.CommandContext(
		ctx,
		"ffmpeg",
		"-y",
		"-i", filePath,
		"-map", "0:v?",
		"-map", "0:a?",
		"-c:v", "libx264",
		"-c:a", "aac",
		"-movflags", "+faststart",
		outputPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", nil, fmt.Errorf("ffmpeg failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	cleanup := func() {
		if removeErr := os.Remove(outputPath); removeErr != nil {
			logger.Warnf("Failed to remove transcoded file %s: %v", outputPath, removeErr)
		}
	}

	return outputPath, cleanup, nil
}
