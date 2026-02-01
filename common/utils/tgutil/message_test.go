package tgutil

import (
	"testing"

	"github.com/gotd/td/tg"
)

func TestGenFileNameFromMessageAvoidsDoubleExtension(t *testing.T) {
	t.Parallel()

	media := &tg.MessageMediaDocument{
		Document: &tg.Document{
			ID:       10,
			MimeType: "application/pdf",
			Attributes: []tg.DocumentAttributeClass{
				&tg.DocumentAttributeFilename{
					FileName: "report.pdf",
				},
			},
		},
	}

	message := tg.Message{
		ID:      99,
		Message: "",
		Media:   media,
	}

	name := GenFileNameFromMessage(message)
	if name != "report.pdf" {
		t.Fatalf("expected file name report.pdf, got %q", name)
	}
}
