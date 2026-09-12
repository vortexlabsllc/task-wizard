package notifier

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"taskwiz.app/core/internal/models"
	"taskwiz.app/core/internal/utils/test"
)

type NotifierTestSuite struct {
	test.DatabaseTestSuite
	repo     *NotificationRepository
	testUser *models.User
	testTask *models.Task
}

func TestNotifierTestSuite(t *testing.T) {
	suite.Run(t, new(NotifierTestSuite))
}

func (s *NotifierTestSuite) SetupTest() {
	s.DatabaseTestSuite.SetupTest()
	s.repo = NewNotificationRepository(s.DBPool)

	s.testUser = &models.User{
		ID:        1,
		CreatedAt: time.Now(),
	}

	err := s.DB.Create(s.testUser).Error
	s.Require().NoError(err)

	now := time.Now()
	dueDate := now.Add(24 * time.Hour)
	s.testTask = &models.Task{
		Title:       "Test Task",
		CreatedBy:   s.testUser.ID,
		IsActive:    true,
		NextDueDate: &dueDate,
		Notification: models.NotificationTriggerOptions{
			Enabled: true,
			DueDate: true,
			PreDue:  true,
			Overdue: true,
		},
	}

	err = s.DB.Create(s.testTask).Error
	s.Require().NoError(err)

	// Create notification settings for user
	settings := &models.NotificationSettings{
		UserID: s.testUser.ID,
		Provider: models.NotificationProvider{
			Provider: models.NotificationProviderGotify,
			URL:      "https://example.com",
			Token:    "test-token",
		},
	}
	err = s.DB.Create(settings).Error
	s.Require().NoError(err)
}

func (s *NotifierTestSuite) TestGetUserNotificationSettings() {
	ctx := context.Background()

	settings, err := s.repo.GetUserNotificationSettings(ctx, s.testUser.ID)
	s.Require().NoError(err)
	s.NotNil(settings)
	s.Equal(s.testUser.ID, settings.UserID)
	s.Equal(models.NotificationProviderGotify, settings.Provider.Provider)
	s.Equal("https://example.com", settings.Provider.URL)
	s.Equal("test-token", settings.Provider.Token)
}

func (s *NotifierTestSuite) TestBatchInsertNotifications() {
	now := time.Now()

	notifications := []models.Notification{
		{
			TaskID:       s.testTask.ID,
			UserID:       s.testUser.ID,
			Type:         models.NotificationTypeDueDate,
			IsSent:       false,
			ScheduledFor: now,
			Text:         "Test notification 1",
		},
		{
			TaskID:       s.testTask.ID,
			UserID:       s.testUser.ID,
			Type:         models.NotificationTypePreDue,
			IsSent:       false,
			ScheduledFor: now.Add(1 * time.Hour),
			Text:         "Test notification 2",
		},
	}

	err := s.repo.BatchInsertNotifications(notifications)
	s.Require().NoError(err)

	var count int64
	err = s.DB.Model(&models.Notification{}).Count(&count).Error
	s.Require().NoError(err)
	s.Equal(int64(2), count)
}

func (s *NotifierTestSuite) TestGenerateNotifications() {
	ctx := context.Background()

	// Delete any existing notifications from setup
	err := s.DB.Where("1=1").Delete(&models.Notification{}).Error
	s.Require().NoError(err)

	s.repo.GenerateNotifications(ctx, s.testTask)

	var notifications []*models.Notification
	err = s.DB.Find(&notifications).Error
	s.Require().NoError(err)

	// Should generate 2 notifications (DueDate and PreDue are both true)
	s.Require().Len(notifications, 2)

	// Verify due date notification
	var dueDateNotification *models.Notification
	var preDueNotification *models.Notification

	for _, n := range notifications {
		if n.ScheduledFor.Equal(*s.testTask.NextDueDate) {
			dueDateNotification = n
		} else {
			preDueNotification = n
		}
	}

	s.NotNil(dueDateNotification)
	s.Equal(s.testTask.ID, dueDateNotification.TaskID)
	s.Equal(s.testUser.ID, dueDateNotification.UserID)
	s.Equal(models.NotificationTypeDueDate, dueDateNotification.Type)
	s.Equal(false, dueDateNotification.IsSent)

	// Verify pre-due notification
	s.NotNil(preDueNotification)
	s.Equal(s.testTask.ID, preDueNotification.TaskID)
	s.Equal(s.testUser.ID, preDueNotification.UserID)
	s.Equal(models.NotificationTypePreDue, preDueNotification.Type)
	s.Equal(false, preDueNotification.IsSent)
	s.Equal(s.testTask.NextDueDate.Add(-3*time.Hour).Unix(), preDueNotification.ScheduledFor.Unix())
}

func (s *NotifierTestSuite) TestMarkNotificationsAsSent() {
	now := time.Now()

	notifications := []*models.Notification{
		{
			TaskID:       s.testTask.ID,
			UserID:       s.testUser.ID,
			Type:         models.NotificationTypeDueDate,
			IsSent:       false,
			ScheduledFor: now,
			Text:         "Test notification 1",
		},
		{
			TaskID:       s.testTask.ID,
			UserID:       s.testUser.ID,
			Type:         models.NotificationTypePreDue,
			IsSent:       false,
			ScheduledFor: now.Add(1 * time.Hour),
			Text:         "Test notification 2",
		},
	}

	err := s.DB.Create(&notifications).Error
	s.Require().NoError(err)

	err = s.repo.MarkNotificationsAsSent(notifications)
	s.Require().NoError(err)

	var count int64
	err = s.DB.Model(&models.Notification{}).Where("is_sent = ?", true).Count(&count).Error
	s.Require().NoError(err)
	s.Equal(int64(2), count)
}

func (s *NotifierTestSuite) TestGetPendingNotification() {
	ctx := context.Background()
	now := time.Now()

	// Create test notifications
	notifications := []*models.Notification{
		{
			TaskID:       s.testTask.ID,
			UserID:       s.testUser.ID,
			Type:         models.NotificationTypeDueDate,
			IsSent:       false,
			ScheduledFor: now.Add(-1 * time.Hour), // This one is pending (scheduled in the past)
			Text:         "Pending notification",
		},
		{
			TaskID:       s.testTask.ID,
			UserID:       s.testUser.ID,
			Type:         models.NotificationTypePreDue,
			IsSent:       true, // Already sent
			ScheduledFor: now.Add(-2 * time.Hour),
			Text:         "Already sent notification",
		},
		{
			TaskID:       s.testTask.ID,
			UserID:       s.testUser.ID,
			Type:         models.NotificationTypePreDue,
			IsSent:       false,
			ScheduledFor: now.Add(1 * time.Hour), // Future notification
			Text:         "Future notification",
		},
	}

	err := s.DB.Create(&notifications).Error
	s.Require().NoError(err)

	pending, err := s.repo.GetPendingNotification(ctx, 24*time.Hour)
	s.Require().NoError(err)
	s.Require().Len(pending, 1)
	s.Equal("Pending notification", pending[0].Text)
	s.Equal(models.NotificationTypeDueDate, pending[0].Type)
}

func (s *NotifierTestSuite) TestGetOverdueTasksWithNotifications() {
	ctx := context.Background()
	now := time.Now()

	// Create an overdue task
	overdueDueDate := now.Add(-24 * time.Hour)
	overdueTask := &models.Task{
		Title:       "Overdue Task",
		CreatedBy:   s.testUser.ID,
		IsActive:    true,
		NextDueDate: &overdueDueDate,
		Notification: models.NotificationTriggerOptions{
			Enabled: true,
			Overdue: true,
		},
	}

	err := s.DB.Create(overdueTask).Error
	s.Require().NoError(err)

	// Create a non-overdue task (future due date)
	futureDueDate := now.Add(24 * time.Hour)
	futureTask := &models.Task{
		Title:       "Future Task",
		CreatedBy:   s.testUser.ID,
		IsActive:    true,
		NextDueDate: &futureDueDate,
		Notification: models.NotificationTriggerOptions{
			Enabled: true,
			Overdue: true,
		},
	}

	err = s.DB.Create(futureTask).Error
	s.Require().NoError(err)

	// Create an inactive overdue task
	inactiveDueDate := now.Add(-12 * time.Hour)
	inactiveTask := &models.Task{
		Title:       "Inactive Task",
		CreatedBy:   s.testUser.ID,
		IsActive:    false, // not active
		NextDueDate: &inactiveDueDate,
		Notification: models.NotificationTriggerOptions{
			Enabled: true,
			Overdue: true,
		},
	}

	err = s.DB.Create(inactiveTask).Error
	s.Require().NoError(err)

	// Create an overdue task with notifications disabled
	noNotifDueDate := now.Add(-12 * time.Hour)
	noNotifTask := &models.Task{
		Title:       "No Notification Task",
		CreatedBy:   s.testUser.ID,
		IsActive:    true,
		NextDueDate: &noNotifDueDate,
		Notification: models.NotificationTriggerOptions{
			Enabled: false, // notifications disabled
		},
	}

	err = s.DB.Create(noNotifTask).Error
	s.Require().NoError(err)

	tasks, err := s.repo.GetOverdueTasksWithNotifications(ctx, now)
	s.Require().NoError(err)
	s.Require().Len(tasks, 2) // Only the tasks that have overdue notification turned on should be returned
	s.Equal(overdueTask.ID, tasks[0].ID)
	s.Equal("Overdue Task", tasks[0].Title)
}

func (s *NotifierTestSuite) TestDeleteSentNotifications() {
	ctx := context.Background()
	now := time.Now()

	// Create test notifications
	notifications := []models.Notification{
		{
			TaskID:       s.testTask.ID,
			UserID:       s.testUser.ID,
			Type:         models.NotificationTypeDueDate,
			IsSent:       true,                     // Already sent
			ScheduledFor: now.Add(-48 * time.Hour), // Old notification
			Text:         "Old sent notification",
		},
		{
			TaskID:       s.testTask.ID,
			UserID:       s.testUser.ID,
			Type:         models.NotificationTypeDueDate,
			IsSent:       true,                    // Already sent
			ScheduledFor: now.Add(-1 * time.Hour), // Recent notification
			Text:         "Recent sent notification",
		},
		{
			TaskID:       s.testTask.ID,
			UserID:       s.testUser.ID,
			Type:         models.NotificationTypeDueDate,
			IsSent:       false,                    // Not sent
			ScheduledFor: now.Add(-72 * time.Hour), // Old notification but not sent
			Text:         "Old unsent notification",
		},
	}

	err := s.DB.Create(&notifications).Error
	s.Require().NoError(err)

	// Delete sent notifications older than 24 hours
	err = s.repo.DeleteSentNotifications(ctx, now.Add(-24*time.Hour))
	s.Require().NoError(err)

	// Verify that only the old sent notification was deleted
	var count int64
	err = s.DB.Model(&models.Notification{}).Count(&count).Error
	s.Require().NoError(err)
	s.Equal(int64(2), count) // One was deleted

	var texts []string
	err = s.DB.Model(&models.Notification{}).Order("text").Pluck("text", &texts).Error
	s.Require().NoError(err)
	s.Equal([]string{"Old unsent notification", "Recent sent notification"}, texts)
}
