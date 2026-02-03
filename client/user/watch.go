package user

import (
	"sync"
	"time"

	"github.com/celestix/gotgproto/dispatcher"
	"github.com/celestix/gotgproto/ext"
	"github.com/charmbracelet/log"
	"github.com/gotd/td/tg"
	"github.com/merisssas/Bot/common/utils/tgutil"
	"github.com/merisssas/Bot/pkg/tfile"
)

// --- Configuration Constants ---
const (
	// AlbumWaitWindow defines the wait time to collect album parts.
	AlbumWaitWindow = 2 * time.Second
	// MaxBufferSize prevents channel blocking under high load.
	MaxBufferSize = 1000
)

// MediaEvent represents a processed media item ready for download.
type MediaEvent struct {
	Ctx       *ext.Context
	ChatID    int64
	MessageID int
	GroupID   int64
	File      tfile.TGFileMessage
	Timestamp time.Time
}

// MediaBatch represents a collection of media (single or album).
type MediaBatch struct {
	ChatID  int64
	GroupID int64
	Events  []MediaEvent
}

type batchManager struct {
	batches  map[int64]*batchBuffer
	mu       sync.Mutex
	outputCh chan MediaBatch
}

type batchBuffer struct {
	timer  *time.Timer
	events []MediaEvent
}

var manager = &batchManager{
	batches:  make(map[int64]*batchBuffer),
	outputCh: make(chan MediaBatch, MaxBufferSize),
}

// GetMediaBatchCh exposes the read-only channel for batch consumers.
func GetMediaBatchCh() <-chan MediaBatch {
	return manager.outputCh
}

func (m *batchManager) ingest(event MediaEvent) {
	if event.GroupID == 0 {
		m.dispatchBatch(MediaBatch{
			ChatID:  event.ChatID,
			GroupID: 0,
			Events:  []MediaEvent{event},
		}, event.Ctx)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	buffer, exists := m.batches[event.GroupID]
	if exists {
		if buffer.timer != nil {
			buffer.timer.Stop()
		}
		buffer.events = append(buffer.events, event)
	} else {
		buffer = &batchBuffer{
			events: []MediaEvent{event},
		}
		m.batches[event.GroupID] = buffer
	}

	buffer.timer = time.AfterFunc(AlbumWaitWindow, func() {
		m.flush(event.GroupID)
	})
}

func (m *batchManager) dispatchBatch(batch MediaBatch, ctx *ext.Context) {
	select {
	case m.outputCh <- batch:
	default:
		if ctx != nil {
			log.FromContext(ctx).Warnf("Media batch channel full, dropping batch group %d", batch.GroupID)
		}
	}
}

func (m *batchManager) flush(groupID int64) {
	m.mu.Lock()
	buffer, exists := m.batches[groupID]
	if !exists {
		m.mu.Unlock()
		return
	}
	delete(m.batches, groupID)
	m.mu.Unlock()

	if len(buffer.events) == 0 {
		return
	}

	m.dispatchBatch(MediaBatch{
		ChatID:  buffer.events[0].ChatID,
		GroupID: groupID,
		Events:  buffer.events,
	}, buffer.events[0].Ctx)
}

func handleMediaMessage(ctx *ext.Context, update *ext.Update) error {
	message := update.EffectiveMessage
	if message == nil {
		return dispatcher.EndGroups
	}
	media, ok := message.GetMedia()
	if !ok || media == nil {
		return dispatcher.EndGroups
	}
	switch media.(type) {
	case *tg.MessageMediaDocument, *tg.MessageMediaPhoto:
	default:
		return dispatcher.EndGroups
	}

	var groupID int64
	if message.GroupedID != 0 {
		groupID = message.GroupedID
	}

	file, err := tfile.FromMediaMessage(media, ctx.Raw, message.Message, tfile.WithNameIfEmpty(
		tgutil.GenFileNameFromMessage(*message.Message),
	))
	if err != nil {
		return err
	}
	chatID := update.EffectiveChat().GetID()
	manager.ingest(MediaEvent{
		Ctx:       ctx,
		ChatID:    chatID,
		MessageID: message.ID,
		GroupID:   groupID,
		File:      file,
		Timestamp: time.Now(),
	})
	return dispatcher.EndGroups
}
