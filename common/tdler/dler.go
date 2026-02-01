package tdler

import (
	"github.com/gotd/td/telegram/downloader"
	"github.com/merisssas/bot/common/utils/dlutil"
	"github.com/merisssas/bot/config"
	"github.com/merisssas/bot/pkg/consts/tglimit"
	"github.com/merisssas/bot/pkg/tfile"
)

func NewDownloader(file tfile.TGFile) *downloader.Builder {
	return downloader.NewDownloader().WithPartSize(tglimit.MaxPartSize).
		Download(file.Dler(), file.Location()).WithThreads(dlutil.BestThreads(file.Size(), config.C().Threads))
}
