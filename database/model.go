package database

import (
	"gorm.io/gorm"
)

type User struct {
	gorm.Model
	ChatID           int64 `gorm:"uniqueIndex;not null"`
	Silent           bool
	DefaultStorage   string
	DefaultDir       uint  // Dir.ID
	Dirs             []Dir `gorm:"constraint:OnDelete:CASCADE"`
	ApplyRule        bool
	Rules            []Rule      `gorm:"constraint:OnDelete:CASCADE"`
	WatchChats       []WatchChat `gorm:"constraint:OnDelete:CASCADE"`
	FilenameStrategy string
	FilenameTemplate string
}

type WatchChat struct {
	gorm.Model
	UserID uint  `gorm:"uniqueIndex:idx_watch_chats_user_chat"`
	ChatID int64 `gorm:"uniqueIndex:idx_watch_chats_user_chat"`
	Filter string
}

type Dir struct {
	gorm.Model
	UserID      uint
	StorageName string
	Path        string
}

type Rule struct {
	gorm.Model
	UserID      uint
	Type        string
	Data        string
	StorageName string
	DirPath     string
}
