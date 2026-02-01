package config

import (
	"github.com/duke-git/lancet/v2/slice"
)

type userConfig struct {
	ID                int64    `toml:"id" mapstructure:"id" json:"id"`                                            // telegram user id
	Storages          []string `toml:"storages" mapstructure:"storages" json:"storages"`                          // storage names
	Blacklist         bool     `toml:"blacklist" mapstructure:"blacklist" json:"blacklist"`                       // 黑名单模式, storage names 中的存储将不会被使用, 默认为白名单模式
	YtdlpSponsorBlock bool     `toml:"ytdlp_sponsorblock" mapstructure:"ytdlp_sponsorblock" json:"ytdlp_sponsor"` // 是否启用 yt-dlp SponsorBlock
	TelegramPremium   bool     `toml:"telegram_premium" mapstructure:"telegram_premium" json:"telegram_premium"`  // 是否启用 Telegram Premium 上限
}

var userIDs []int64
var storages []string
var userStorages = make(map[int64][]string)
var userConfigs = make(map[int64]userConfig)

func (c Config) GetStorageNamesByUserID(userID int64) []string {
	us, ok := userStorages[userID]
	if ok {
		return us
	}
	return nil
}

func (c Config) GetUsersID() []int64 {
	return userIDs
}

func (c Config) HasStorage(userID int64, storageName string) bool {
	us, ok := userStorages[userID]
	if !ok {
		return false
	}
	return slice.Contain(us, storageName)
}

func (c Config) GetUserConfig(userID int64) (userConfig, bool) {
	cfg, ok := userConfigs[userID]
	return cfg, ok
}
