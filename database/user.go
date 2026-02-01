package database

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func CreateUser(ctx context.Context, chatID int64) error {
	if _, err := GetUserByChatID(ctx, chatID); err == nil {
		return nil
	}
	return db.Create(&User{ChatID: chatID}).Error
}

func GetAllUsers(ctx context.Context) ([]User, error) {
	var users []User
	err := db.WithContext(ctx).
		Find(&users).Error
	return users, err
}

func GetUserByChatID(ctx context.Context, chatID int64) (*User, error) {
	var user User
	err := db.WithContext(ctx).
		Where("chat_id = ?", chatID).First(&user).Error
	return &user, err
}

func UpdateUser(ctx context.Context, user *User) error {
	if _, err := GetUserByChatID(ctx, user.ChatID); err != nil {
		return err
	}
	return db.WithContext(ctx).Save(user).Error
}

func DeleteUser(ctx context.Context, user *User) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", user.ID).Delete(&Dir{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", user.ID).Delete(&Rule{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", user.ID).Delete(&WatchChat{}).Error; err != nil {
			return err
		}
		return tx.Select(clause.Associations).Delete(user).Error
	})
}

func GetUserByID(ctx context.Context, id uint) (*User, error) {
	var user User
	err := db.WithContext(ctx).
		Where("id = ?", id).First(&user).Error
	return &user, err
}
