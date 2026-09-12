package repos

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"taskwiz.app/core/config"
	"taskwiz.app/core/internal/models"
	database "taskwiz.app/core/internal/utils/database"
)

var ErrDisabledUser = errors.New("account is disabled")

type IUserRepo interface {
	CreateUser(c context.Context, user *models.User) error
	GetUser(c context.Context, id int) (*models.User, error)
	FindByEntraID(c context.Context, directoryID string, objectID string) (*models.User, error)
	EnsureUser(c context.Context, directoryID string, objectID string) (*models.User, error)
	UpdateNotificationSettings(c context.Context, userID int, provider models.NotificationProvider, triggers models.NotificationTriggerOptions) error
	DeleteNotificationsForUser(c context.Context, userID int) error
	GetLastCreatedOrModifiedForUserResources(c context.Context, userID int) (string, error)
	RequestDeletion(c context.Context, userID int) error
	CancelDeletion(c context.Context, userID int) error
	FindUsersForDeletion(c context.Context, gracePeriod time.Duration) ([]models.User, error)
	DeleteUser(c context.Context, userID int) error
}

type UserRepository struct {
	cfg *config.Config
	db  *database.DB
}

var _ IUserRepo = (*UserRepository)(nil)

func NewUserRepository(db *database.DB, cfg *config.Config) IUserRepo {
	return &UserRepository{cfg, db}
}

func (r *UserRepository) CreateUser(c context.Context, user *models.User) error {
	if !r.cfg.Server.Registration {
		return fmt.Errorf("new account registration is disabled")
	}

	return r.db.RW().WithContext(c).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&user).Error; err != nil {
			return err
		}

		if err := tx.Create(&models.NotificationSettings{
			UserID: user.ID,
			Provider: models.NotificationProvider{
				Provider: models.NotificationProviderNone,
			},
		}).Error; err != nil {
			return err
		}

		return nil
	})
}

func (r *UserRepository) GetUser(c context.Context, id int) (*models.User, error) {
	var user *models.User
	if err := r.db.R().WithContext(c).Where("ID = ?", id).First(&user).Error; err != nil {
		return nil, err
	}
	return user, nil
}

func (r *UserRepository) FindByEntraID(c context.Context, directoryID string, objectID string) (*models.User, error) {
	var user *models.User
	if err := r.db.R().WithContext(c).Where("directory_id = ? AND object_id = ?", directoryID, objectID).First(&user).Error; err != nil {
		return nil, err
	}
	return user, nil
}

func (r *UserRepository) EnsureUser(c context.Context, directoryID string, objectID string) (*models.User, error) {
	// The read-then-write path must stay on the primary (RW) pool so the
	// existence check and any insert observe each other (read-your-writes).
	var user models.User
	err := r.db.RW().WithContext(c).Where("directory_id = ? AND object_id = ?", directoryID, objectID).First(&user).Error
	if err == nil {
		if user.Disabled {
			return nil, ErrDisabledUser
		}
		return &user, nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("find user by Entra ID: %s", err.Error())
	}

	newUser := &models.User{
		DirectoryID: directoryID,
		ObjectID:    objectID,
	}

	if err := r.CreateUser(c, newUser); err != nil {
		return nil, fmt.Errorf("create user: %s", err.Error())
	}

	return newUser, nil
}

func (r *UserRepository) UpdateNotificationSettings(c context.Context, userID int, provider models.NotificationProvider, triggers models.NotificationTriggerOptions) error {
	return r.db.RW().WithContext(c).Where("user_id = ?", userID).Updates(&models.NotificationSettings{
		Provider: provider,
		Triggers: triggers,
	}).Error
}

func (r *UserRepository) DeleteNotificationsForUser(c context.Context, userID int) error {
	return r.db.RW().WithContext(c).Where("user_id = ?", userID).Delete(&models.NotificationSettings{}).Error
}

func (r *UserRepository) GetLastCreatedOrModifiedForUserResources(c context.Context, userID int) (string, error) {
	// Drives client sync decisions (replica lag could cause resyncs), so it
	// stays on the primary (RW) pool.
	var result string
	err := r.db.RW().WithContext(c).Raw(`
		SELECT 
			MAX(
				COALESCE(MAX(updated_at), '1970-01-01 00:00:00'),
				COALESCE(MAX(created_at), '1970-01-01 00:00:00')
			) AS last_modified
		FROM (
			SELECT updated_at, created_at FROM labels WHERE created_by = ?
			UNION ALL
			SELECT updated_at, created_at FROM tasks WHERE created_by = ?
		) AS combined_dates
	`, userID, userID).Scan(&result).Error

	return result, err
}

func (r *UserRepository) RequestDeletion(c context.Context, userID int) error {
	now := time.Now().UTC()
	return r.db.RW().WithContext(c).Model(&models.User{}).Where("id = ?", userID).Update("deletion_requested_at", now).Error
}

func (r *UserRepository) CancelDeletion(c context.Context, userID int) error {
	return r.db.RW().WithContext(c).Model(&models.User{}).Where("id = ?", userID).Update("deletion_requested_at", nil).Error
}

func (r *UserRepository) FindUsersForDeletion(c context.Context, gracePeriod time.Duration) ([]models.User, error) {
	threshold := time.Now().UTC().Add(-gracePeriod)
	var users []models.User
	err := r.db.RO().WithContext(c).
		Where("deletion_requested_at IS NOT NULL AND deletion_requested_at <= ?", threshold).
		Find(&users).Error
	return users, err
}

func (r *UserRepository) DeleteUser(c context.Context, userID int) error {
	return r.db.RW().WithContext(c).Delete(&models.User{}, userID).Error
}
