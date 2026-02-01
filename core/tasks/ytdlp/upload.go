package ytdlp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/celestix/gotgproto/ext"
	"github.com/charmbracelet/log"
	"github.com/gabriel-vasile/mimetype"
	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"

	"github.com/merisssas/bot/common/utils/dlutil"
	"github.com/merisssas/bot/common/utils/tgutil"
	"github.com/merisssas/bot/config"
)

const (
	telegramBotUploadLimit     = 2 * 1024 * 1024 * 1024
	telegramPremiumUploadLimit = 4 * 1024 * 1024 * 1024
)

func (t *Task) uploadFilesToChat(ctx context.Context, filePaths []string, tempDir string) error {
	if len(filePaths) == 0 {
		return nil
	}
	tctx := tgutil.ExtFromContext(ctx)
	if tctx == nil {
		return fmt.Errorf("failed to get telegram context")
	}
	peer := tryGetInputPeer(tctx, t.ChatID)
	if peer == nil || peer.Zero() {
		return fmt.Errorf("failed to get input peer for chat ID %d", t.ChatID)
	}

	for _, filePath := range filePaths {
		if err := t.uploadFileToChat(ctx, tctx, peer, filePath, tempDir); err != nil {
			return err
		}
	}
	return nil
}

func (t *Task) uploadFileToChat(ctx context.Context, tctx *ext.Context, peer tg.InputPeerClass, filePath, tempDir string) error {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to stat file %s: %w", filePath, err)
	}
	size := fileInfo.Size()
	if maxSize := t.maxTelegramUploadSize(); maxSize > 0 && size > maxSize {
		return fmt.Errorf("file %s exceeds telegram upload limit: %d bytes", filepath.Base(filePath), size)
	}

	filename := filepath.Base(filePath)
	if t.Progress != nil {
		t.Progress.OnProgress(ctx, t, fmt.Sprintf("Uploading to Telegram: %s", filename))
	}

	upler := uploader.NewUploader(tctx.Raw).
		WithThreads(dlutil.BestThreads(size, config.C().Threads))

	inputFile, err := upler.FromPath(ctx, filePath)
	if err != nil {
		return fmt.Errorf("failed to upload file %s: %w", filePath, err)
	}

	caption := styling.Plain(filename)
	doc := message.UploadedDocument(inputFile, caption).Filename(filename)

	mtype, err := mimetype.DetectFile(filePath)
	if err != nil {
		log.FromContext(ctx).Warnf("Failed to detect mimetype for %s: %v", filename, err)
	}

	var media message.MediaOption = doc
	if mtype != nil {
		switch {
		case strings.HasPrefix(mtype.String(), "audio/"):
			media = doc.Audio().Title(filename)
		case strings.HasPrefix(mtype.String(), "video/"):
			if thumbPath := findThumbnailPath(tempDir, filePath); thumbPath != "" {
				thumb, err := upler.FromPath(ctx, thumbPath)
				if err != nil {
					log.FromContext(ctx).Warnf("Failed to upload thumbnail %s: %v", thumbPath, err)
				} else {
					doc = doc.Thumb(thumb)
				}
			}
			media = doc.Video().SupportsStreaming()
		}
	}

	_, err = tctx.Sender.WithUploader(upler).To(peer).Media(ctx, media)
	if err != nil {
		return fmt.Errorf("failed to send file %s to telegram: %w", filename, err)
	}
	return nil
}

func (t *Task) maxTelegramUploadSize() int64 {
	return MaxTelegramUploadSize(t.TelegramPremium)
}

func MaxTelegramUploadSize(premium bool) int64 {
	if premium {
		return telegramPremiumUploadLimit
	}
	return telegramBotUploadLimit
}

func findThumbnailPath(tempDir, mediaPath string) string {
	baseName := strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))
	candidates := []string{
		filepath.Join(tempDir, baseName+".jpg"),
		filepath.Join(tempDir, baseName+".jpeg"),
		filepath.Join(tempDir, baseName+".png"),
		filepath.Join(tempDir, baseName+".webp"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func tryGetInputPeer(ctx *ext.Context, chatID int64) tg.InputPeerClass {
	peer := ctx.PeerStorage.GetInputPeerById(chatID)
	if peer != nil && !peer.Zero() {
		return peer
	}
	id := constant.TDLibPeerID(chatID)
	plain := id.ToPlain()
	var channel constant.TDLibPeerID
	channel.Channel(plain)
	peer = ctx.PeerStorage.GetInputPeerById(int64(channel))
	if peer != nil && !peer.Zero() {
		return peer
	}
	var chat constant.TDLibPeerID
	chat.Chat(plain)
	peer = ctx.PeerStorage.GetInputPeerById(int64(chat))
	if peer != nil && !peer.Zero() {
		return peer
	}
	var user constant.TDLibPeerID
	user.User(plain)
	return ctx.PeerStorage.GetInputPeerById(int64(user))
}
