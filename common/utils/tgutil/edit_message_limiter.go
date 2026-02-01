package tgutil

import (
	"errors"
	"sync"
	"time"

	"github.com/celestix/gotgproto/ext"
	"github.com/celestix/gotgproto/types"
	"github.com/gotd/td/tg"
	"golang.org/x/time/rate"
)

const (
	editMessageQueueSize = 256
	editMessageInterval  = 200 * time.Millisecond
)

type editMessageRequest struct {
	ctx    *ext.Context
	chatID int64
	req    *tg.MessagesEditMessageRequest
	resp   chan editMessageResult
}

type editMessageResult struct {
	msg *types.Message
	err error
}

var (
	editMessageQueue       = make(chan editMessageRequest, editMessageQueueSize)
	editMessageRateLimiter = rate.NewLimiter(rate.Every(editMessageInterval), 1)
	editMessageWorkerOnce  sync.Once
)

func EditMessage(ctx *ext.Context, chatID int64, req *tg.MessagesEditMessageRequest) (*types.Message, error) {
	if ctx == nil {
		return nil, errors.New("edit message: nil context")
	}
	editMessageWorkerOnce.Do(func() {
		go editMessageWorker()
	})
	resp := make(chan editMessageResult, 1)
	request := editMessageRequest{
		ctx:    ctx,
		chatID: chatID,
		req:    req,
		resp:   resp,
	}
	select {
	case editMessageQueue <- request:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case result := <-resp:
		return result.msg, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func editMessageWorker() {
	for request := range editMessageQueue {
		if request.ctx == nil {
			request.resp <- editMessageResult{err: errors.New("edit message: nil context")}
			continue
		}
		if err := editMessageRateLimiter.Wait(request.ctx); err != nil {
			request.resp <- editMessageResult{err: err}
			continue
		}
		msg, err := request.ctx.EditMessage(request.chatID, request.req)
		request.resp <- editMessageResult{msg: msg, err: err}
	}
}
