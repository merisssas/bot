package upload

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/charmbracelet/log"
	"github.com/merisssas/Bot/client/bot"
	"github.com/merisssas/Bot/common/cache"
	"github.com/merisssas/Bot/common/i18n"
	"github.com/merisssas/Bot/common/utils/ioutil"
	"github.com/merisssas/Bot/common/utils/tgutil"
	"github.com/merisssas/Bot/config"
	"github.com/merisssas/Bot/database"
	"github.com/merisssas/Bot/pkg/enums/ctxkey"
	stortype "github.com/merisssas/Bot/pkg/enums/storage"
	"github.com/merisssas/Bot/storage"
	"github.com/spf13/cobra"
)

var uploadCmd = &cobra.Command{
	Use:   "upload",
	Short: "upload local files to storage",
	RunE:  Upload,
}

func Register(root *cobra.Command) {
	uploadCmd.Flags().StringP("file", "f", "", "file path to upload")
	uploadCmd.MarkFlagRequired("file")
	uploadCmd.Flags().StringP("storage", "s", "", "storage name to upload to")
	uploadCmd.MarkFlagRequired("storage")
	uploadCmd.Flags().StringP("dir", "d", "", "storage dir to upload to, default is the base_path of the storage")
	uploadCmd.Flags().Bool("no-progress", false, "disable progress bar")
	root.AddCommand(uploadCmd)
}

func Upload(cmd *cobra.Command, args []string) error {
	storname, err := cmd.Flags().GetString("storage")
	if err != nil {
		return err
	}
	fp, err := cmd.Flags().GetString("file")
	if err != nil {
		return err
	}
	dirPath, err := cmd.Flags().GetString("dir")
	if err != nil {
		return err
	}
	noProgress, err := cmd.Flags().GetBool("no-progress")
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	log := log.FromContext(ctx)
	configFile := config.GetConfigFile(cmd)
	if err := config.Init(ctx, configFile); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	i18n.Init(config.C().Lang)
	if err := cache.Init(cache.Config{
		NumCounters: config.C().Cache.NumCounters,
		MaxCost:     config.C().Cache.MaxCost,
		BufferItems: 64,
		DefaultTTL:  time.Duration(config.C().Cache.TTL) * time.Second,
	}, log); err != nil {
		return fmt.Errorf("failed to init cache: %w", err)
	}
	database.Init(ctx)

	stor, err := storage.GetStorageByName(ctx, storname)
	if err != nil {
		log.Fatal("Failed to get storage", "error", err)
	}

	switch stor.Type() {
	case stortype.Telegram:
		bot.Init(ctx)
	default:
		// placeholder for other storage types that may need special initialization
	}

	file, err := os.Open(filepath.Clean(fp))
	if err != nil {
		log.Fatal("Failed to open file", "error", err)
	}
	defer file.Close()
	fileInfo, err := file.Stat()
	if err != nil {
		log.Fatal("Failed to get file info", "error", err)
	}
	fileName := fileInfo.Name()
	fileSize := fileInfo.Size()

	uploadPath := path.Join(dirPath, fileName)

	ctx = context.WithValue(ctx, ctxkey.ContentLength, fileSize)
	ctx = tgutil.ExtWithContext(ctx, bot.ExtContext())

	// Create progress reader and UI
	var reader io.Reader
	var progressUI *UploadProgress
	log.Info("Uploading file...", "file", fp, "to", storname, "as", uploadPath)

	if !noProgress && fileSize > 0 {
		progressUI = NewUploadProgress(ctx, fileName, fileSize)
		progressUI.Start()

		reader = ioutil.NewProgressReader(file, fileSize, func(read int64, total int64) {
			if total > 0 {
				progressUI.UpdateProgress(float64(read) / float64(total))
			}
		})
	} else {
		reader = file
	}

	if err := stor.Save(ctx, reader, uploadPath); err != nil {
		if progressUI != nil {
			progressUI.SetError(err)
			progressUI.Wait()
		}
		log.Fatal("Failed to upload file", "error", err)
	}

	if progressUI != nil {
		progressUI.Done()
		progressUI.Wait()
	}
	log.Info("File uploaded successfully")
	return nil
}
