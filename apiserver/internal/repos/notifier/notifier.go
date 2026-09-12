package notifier

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"taskwiz.app/core/internal/models"
	"taskwiz.app/core/internal/services/logging"
	database "taskwiz.app/core/internal/utils/database"
)

// Connection routing for this repository:
//   - RW (primary): all writes (inserts, marking, deletions)
//   - R: ordinary reads (notification settings)
//   - RO: heavy scheduled scans (pending/overdue/missing references)
type NotificationRepository struct {
	db *database.DB
}

func NewNotificationRepository(db *database.DB) *NotificationRepository {
	return &NotificationRepository{db}
}

func (r *NotificationRepository) GetUserNotificationSettings(c context.Context, userID int) (*models.NotificationSettings, error) {
	var settings models.NotificationSettings
	if err := r.db.R().WithContext(c).Model(&models.NotificationSettings{}).First(&settings, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &models.NotificationSettings{UserID: userID}, nil
		}
		return nil, err
	}
	return &settings, nil
}

func (r *NotificationRepository) DeleteNotificationsByIDs(c context.Context, ids []int) error {
	if len(ids) == 0 {
		return nil
	}

	return r.db.RW().WithContext(c).Where("id IN ?", ids).Delete(&models.Notification{}).Error
}

func (r *NotificationRepository) deleteAllTaskNotifications(taskID int) error {
	return r.db.RW().Where("task_id = ?", taskID).Delete(&models.Notification{}).Error
}

func (r *NotificationRepository) GetNotificationsWithMissingUserOrTask(c context.Context) ([]*models.Notification, error) {
	var notifications []*models.Notification
	err := r.db.RO().WithContext(c).
		Raw(`
			SELECT n.*
			FROM notifications n
			LEFT JOIN users u ON n.user_id = u.id
			LEFT JOIN tasks t ON n.task_id = t.id
			WHERE u.id IS NULL OR t.id IS NULL
		`).Scan(&notifications).Error
	if err != nil {
		return nil, err
	}

	return notifications, nil
}

func (r *NotificationRepository) BatchInsertNotifications(notifications []models.Notification) error {
	return r.db.RW().Create(notifications).Error
}

func (r *NotificationRepository) GenerateNotifications(c context.Context, task *models.Task) {
	log := logging.FromContext(c)
	if err := r.deleteAllTaskNotifications(task.ID); err != nil {
		log.Errorf("failed to delete existing notifications: %s", err.Error())
		return
	}

	ns := task.Notification
	if !ns.Enabled {
		return
	}

	if task.NextDueDate == nil {
		return
	}

	notifications := make([]models.Notification, 0)
	if ns.DueDate {
		notifications = append(notifications, models.Notification{
			TaskID:       task.ID,
			UserID:       task.CreatedBy,
			Type:         models.NotificationTypeDueDate,
			IsSent:       false,
			ScheduledFor: *task.NextDueDate,
			Text:         fmt.Sprintf("📅 *%s* is due", task.Title),
		})
	}

	if ns.PreDue {
		notifications = append(notifications, models.Notification{
			TaskID:       task.ID,
			UserID:       task.CreatedBy,
			Type:         models.NotificationTypePreDue,
			IsSent:       false,
			ScheduledFor: task.NextDueDate.Add(-time.Hour * 3),
			Text:         fmt.Sprintf("📢 *%s* is coming up on %s", task.Title, task.NextDueDate.Format("January 2nd")),
		})
	}

	if len(notifications) > 0 {
		if err := r.BatchInsertNotifications(notifications); err != nil {
			log.Errorf("failed to insert new notifications: %s", err.Error())
			return
		}
	}
}

func (r *NotificationRepository) MarkNotificationsAsSent(notifications []*models.Notification) error {
	var ids []int
	for _, notification := range notifications {
		ids = append(ids, notification.ID)
	}

	return r.db.RW().Model(&models.Notification{}).Where("id IN (?)", ids).Update("is_sent", true).Error
}

func (r *NotificationRepository) GetPendingNotification(c context.Context, lookback time.Duration) ([]*models.Notification, error) {
	var notifications []*models.Notification
	cutoff := time.Now()
	if err := r.db.RO().Where("is_sent = 0 AND scheduled_for < ?", cutoff).Preload("User.NotificationSettings").Find(&notifications).Error; err != nil {
		return nil, err
	}
	return notifications, nil
}

func (r *NotificationRepository) GetOverdueTasksWithNotifications(c context.Context, now time.Time) ([]*models.Task, error) {
	var tasks []*models.Task
	if err := r.db.RO().WithContext(c).Where("is_active = 1 AND next_due_date <= ? AND notification_overdue = 1", now).Select("id, created_by, title").Find(&tasks).Error; err != nil {
		return nil, err
	}

	return tasks, nil
}

func (r *NotificationRepository) DeleteSentNotifications(c context.Context, since time.Time) error {
	return r.db.RW().WithContext(c).Where("is_sent = 1 AND scheduled_for < ?", since).Delete(&models.Notification{}).Error
}
