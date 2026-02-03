package tdler

import (
	"github.com/gotd/td/telegram/downloader"
	"github.com/merisssas/Bot/common/utils/dlutil"
	"github.com/merisssas/Bot/config"
	"github.com/merisssas/Bot/pkg/consts/tglimit"
	"github.com/merisssas/Bot/pkg/tfile"
)

func NewDownloader(file tfile.TGFile) *downloader.Builder {
	return downloader.NewDownloader().WithPartSize(tglimit.MaxPartSize).
		Download(file.Dler(), file.Location()).WithThreads(dlutil.BestThreads(file.Size(), config.C().Threads))
}
