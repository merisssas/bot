package config

import (
	"github.com/duke-git/lancet/v2/slice"
)

type userConfig struct {
	ID        int64    `toml:"id" mapstructure:"id" json:"id"`                      // telegram user id
	Storages  []string `toml:"storages" mapstructure:"storages" json:"storages"`    // storage names
	Blacklist bool     `toml:"blacklist" mapstructure:"blacklist" json:"blacklist"` // Blacklist mode: storages in the list will be denied (default is allowlist mode).
}

func (c Config) GetStorageNamesByUserID(userID int64) []string {
	us, ok := c.userStorages[userID]
	if ok {
		return us
	}
	return nil
}

func (c Config) GetUsersID() []int64 {
	return c.userIDs
}

func (c Config) HasStorage(userID int64, storageName string) bool {
	us, ok := c.userStorages[userID]
	if !ok {
		return false
	}
	return slice.Contain(us, storageName)
}
