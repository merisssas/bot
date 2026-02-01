package tgutil

import (
	"testing"

	"github.com/gotd/td/tg"
)

func TestGetMediaFileNameDocumentUsesMimeExtension(t *testing.T) {
	t.Parallel()

	media := &tg.MessageMediaDocument{
		Document: &tg.Document{
			ID:       42,
			MimeType: "application/pdf",
		},
	}

	name, err := GetMediaFileName(media)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if name != "42.pdf" {
		t.Fatalf("expected file name 42.pdf, got %q", name)
	}
}

func TestGetMediaFileNamePhotoUsesJpgExtension(t *testing.T) {
	t.Parallel()

	media := &tg.MessageMediaPhoto{
		Photo: &tg.Photo{
			ID: 7,
		},
	}

	name, err := GetMediaFileName(media)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if name != "7.jpg" {
		t.Fatalf("expected file name 7.jpg, got %q", name)
	}
}
