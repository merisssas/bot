package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"slices"

	"github.com/charmbracelet/log"
	"github.com/merisssas/bot/client/bot"
	userclient "github.com/merisssas/bot/client/user"
	"github.com/merisssas/bot/common/cache"
	"github.com/merisssas/bot/common/i18n"
	"github.com/merisssas/bot/common/utils/fsutil"
	"github.com/merisssas/bot/config"
	"github.com/merisssas/bot/core"
	"github.com/merisssas/bot/core/tasks/aria2dl"
	"github.com/merisssas/bot/core/tasks/directlinks"
	"github.com/merisssas/bot/database"
	"github.com/merisssas/bot/parsers"
	"github.com/merisssas/bot/storage"
	"github.com/spf13/cobra"
)

func Run(cmd *cobra.Command, _ []string) {
	ctx, cancel := context.WithCancel(cmd.Context())
	logger := log.NewWithOptions(os.Stdout, log.Options{
		Level:           log.DebugLevel,
		ReportTimestamp: true,
		TimeFormat:      time.TimeOnly,
		ReportCaller:    true,
	})
	ctx = log.WithContext(ctx, logger)

	exitChan, err := initAll(ctx, cmd)
	if err != nil {
		logger.Fatal("Init failed", "error", err)
	}
	go func() {
		<-exitChan
		cancel()
	}()

	core.Run(ctx)

	<-ctx.Done()
	logger.Info("Exiting...")
	defer logger.Info("Exit complete")
	cleanCache()
}

func initAll(ctx context.Context, cmd *cobra.Command) (<-chan struct{}, error) {
	configFile := config.GetConfigFile(cmd)
	if err := config.Init(ctx, configFile); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	if err := cache.Init(); err != nil {
		return nil, fmt.Errorf("failed to init cache: %w", err)
	}
	logger := log.FromContext(ctx)
	if err := i18n.Init(config.C().Lang); err != nil {
		logger.Warn("Failed to init i18n", "error", err)
	}
	logger.Info("Initializing...")
	database.Init(ctx)
	storage.LoadStorages(ctx)
	aria2dl.StartupCleanup(ctx)
	directlinks.RestorePersistedTasks(ctx)
	if config.C().Parser.PluginEnable {
		for _, dir := range config.C().Parser.PluginDirs {
			if err := parsers.LoadPlugins(ctx, dir); err != nil {
				logger.Error("Failed to load parser plugins", "dir", dir, "error", err)
			} else {
				logger.Debug("Loaded parser plugins from directory", "dir", dir)
			}
		}
	}
	if config.C().Telegram.Userbot.Enable {
		_, err := userclient.Login(ctx)
		if err != nil {
			logger.Fatal("User login failed", "error", err)
		}
	}
	return bot.Init(ctx), nil
}

func cleanCache() {
	if config.C().NoCleanCache {
		return
	}
	if config.C().Temp.BasePath != "" && !config.C().Stream {
		cleanedBasePath := filepath.Clean(config.C().Temp.BasePath)
		if filepath.IsAbs(cleanedBasePath) {
			log.Error("Cache directory must be relative", "path", config.C().Temp.BasePath)
			return
		}
		if slices.Contains([]string{".", "..", string(os.PathSeparator)}, cleanedBasePath) ||
			strings.HasPrefix(cleanedBasePath, fmt.Sprintf("..%c", os.PathSeparator)) {
			log.Error("Invalid cache directory", "path", config.C().Temp.BasePath)
			return
		}
		currentDir, err := os.Getwd()
		if err != nil {
			log.Error("Failed to get working directory", "error", err)
			return
		}
		cachePath := filepath.Join(currentDir, cleanedBasePath)
		cachePath, err = filepath.Abs(cachePath)
		if err != nil {
			log.Error("Failed to get absolute cache path", "error", err)
			return
		}
		relPath, err := filepath.Rel(currentDir, cachePath)
		if err != nil {
			log.Error("Failed to validate cache path", "error", err)
			return
		}
		if relPath == ".." || strings.HasPrefix(relPath, fmt.Sprintf("..%c", os.PathSeparator)) {
			log.Error("Cache directory escapes working directory", "path", cachePath)
			return
		}
		log.Info("Cleaning cache directory", "path", cachePath)
		if err := fsutil.RemoveAllInDir(cachePath); err != nil {
			log.Error("Failed to clean cache directory", "error", err)
		}
	}
}
