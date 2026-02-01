package ytdlp

import "testing"

func TestSanitizeFlagsRejectsUnsafe(t *testing.T) {
	t.Parallel()

	tests := []string{
		"--exec=echo",
		"--output",
		"-o",
		"--paths=/tmp",
		"-P",
		"--external-downloader",
		"--external-downloader-args=foo",
		"--downloader",
		"--downloader-args=aria2c:-x16",
		"--postprocessor-args",
		"--ppa=ffmpeg:-v",
		"--ffmpeg-location=/tmp/ffmpeg",
		"--use-postprocessor=foo",
		"--exec-after-downloads",
	}

	for _, flag := range tests {
		_, err := sanitizeFlags([]string{flag})
		if err == nil {
			t.Errorf("expected error for unsafe flag %q", flag)
		}
	}
}

func TestSanitizeFlagsAllowsSafe(t *testing.T) {
	t.Parallel()

	flags, err := sanitizeFlags([]string{"--format", "best"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(flags) != 2 {
		t.Fatalf("expected 2 flags, got %d", len(flags))
	}
}

func TestIsIgnoredDownloadArtifact(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"video.mp4":            false,
		"video.mp4.part":       true,
		"video.info.json":      true,
		"thumb.jpg":            true,
		"subtitle.en.vtt":      true,
		"metadata.ytdl":        true,
		"notes.description":    true,
		"annotations.xml":      false,
		"file.ass":             true,
		"file.tmp":             true,
		"file.webp":            true,
		"file.srt":             true,
		"file.jpeg":            true,
		"file.png":             true,
		"file.lrc":             true,
		"file.description":     true,
		"file.annotations.xml": true,
	}

	for name, expected := range cases {
		if isIgnoredDownloadArtifact(name) != expected {
			t.Errorf("expected isIgnoredDownloadArtifact(%q) to be %t", name, expected)
		}
	}
}
