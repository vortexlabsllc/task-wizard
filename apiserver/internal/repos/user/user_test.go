package repos

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"taskwiz.app/core/config"
	"taskwiz.app/core/internal/models"
	"taskwiz.app/core/internal/utils/test"
)

type UserTestSuite struct {
	test.DatabaseTestSuite
	repo IUserRepo
	cfg  *config.Config
}

func TestUserTestSuite(t *testing.T) {
	suite.Run(t, new(UserTestSuite))
}

func (s *UserTestSuite) SetupTest() {
	s.DatabaseTestSuite.SetupTest()

	s.cfg = &config.Config{
		Server: config.ServerConfig{
			Registration: true,
		},
	}
	s.repo = NewUserRepository(s.DBPool, s.cfg)
}

func (s *UserTestSuite) TestCreateUser() {
	ctx := context.Background()

	user := &models.User{
		DirectoryID: "test-dir",
		ObjectID:    "test-obj",
		CreatedAt:   time.Now(),
	}

	err := s.repo.CreateUser(ctx, user)
	s.Require().NoError(err)
	s.NotZero(user.ID)

	var fetchedUser models.User
	err = s.DB.First(&fetchedUser, user.ID).Error
	s.Require().NoError(err)
	s.Equal(user.DirectoryID, fetchedUser.DirectoryID)
	s.Equal(user.ObjectID, fetchedUser.ObjectID)

	var settings models.NotificationSettings
	err = s.DB.Where("user_id = ?", user.ID).First(&settings).Error
	s.Require().NoError(err)
	s.Equal(user.ID, settings.UserID)
	s.Equal(models.NotificationProviderNone, settings.Provider.Provider)
}

func (s *UserTestSuite) TestCreateUserRegistrationDisabled() {
	ctx := context.Background()

	s.cfg.Server.Registration = false

	user := &models.User{
		DirectoryID: "test-dir",
		ObjectID:    "test-obj",
	}

	err := s.repo.CreateUser(ctx, user)
	s.Require().Error(err)
	s.Contains(err.Error(), "registration is disabled")
}

func (s *UserTestSuite) TestGetUser() {
	ctx := context.Background()

	testUser := &models.User{
		DirectoryID: "test-dir",
		ObjectID:    "test-obj",
		CreatedAt:   time.Now(),
	}

	err := s.DB.Create(testUser).Error
	s.Require().NoError(err)

	user, err := s.repo.GetUser(ctx, testUser.ID)
	s.Require().NoError(err)
	s.NotNil(user)
	s.Equal(testUser.ID, user.ID)
}

func (s *UserTestSuite) TestFindByEntraID() {
	ctx := context.Background()

	testUser := &models.User{
		DirectoryID: "dir-123",
		ObjectID:    "obj-456",
		CreatedAt:   time.Now(),
	}

	err := s.DB.Create(testUser).Error
	s.Require().NoError(err)

	user, err := s.repo.FindByEntraID(ctx, "dir-123", "obj-456")
	s.Require().NoError(err)
	s.NotNil(user)
	s.Equal(testUser.ID, user.ID)
}

func (s *UserTestSuite) TestFindByEntraIDNotFound() {
	ctx := context.Background()

	_, err := s.repo.FindByEntraID(ctx, "nonexistent-dir", "nonexistent-obj")
	s.Require().Error(err)
}

func (s *UserTestSuite) TestEnsureUserCreatesNew() {
	ctx := context.Background()

	user, err := s.repo.EnsureUser(ctx, "new-dir", "new-obj")
	s.Require().NoError(err)
	s.NotNil(user)
	s.NotZero(user.ID)
	s.Equal("new-dir", user.DirectoryID)
	s.Equal("new-obj", user.ObjectID)
}

func (s *UserTestSuite) TestEnsureUserReturnsExisting() {
	ctx := context.Background()

	existing := &models.User{
		DirectoryID: "existing-dir",
		ObjectID:    "existing-obj",
		CreatedAt:   time.Now(),
	}

	err := s.DB.Create(existing).Error
	s.Require().NoError(err)

	user, err := s.repo.EnsureUser(ctx, "existing-dir", "existing-obj")
	s.Require().NoError(err)
	s.Equal(existing.ID, user.ID)
}

func (s *UserTestSuite) TestEnsureUserDisabled() {
	ctx := context.Background()

	disabled := &models.User{
		DirectoryID: "disabled-dir",
		ObjectID:    "disabled-obj",
		Disabled:    true,
		CreatedAt:   time.Now(),
	}

	err := s.DB.Create(disabled).Error
	s.Require().NoError(err)

	_, err = s.repo.EnsureUser(ctx, "disabled-dir", "disabled-obj")
	s.Require().Error(err)
	s.ErrorIs(err, ErrDisabledUser)
}

func (s *UserTestSuite) TestEnsureUserRegistrationDisabled() {
	ctx := context.Background()

	s.cfg.Server.Registration = false

	_, err := s.repo.EnsureUser(ctx, "new-dir", "new-obj")
	s.Require().Error(err)
	s.Contains(err.Error(), "registration is disabled")
}

func (s *UserTestSuite) TestUpdateNotificationSettings() {
	ctx := context.Background()

	testUser := &models.User{
		DirectoryID: "test-dir",
		ObjectID:    "test-obj",
		CreatedAt:   time.Now(),
	}

	err := s.DB.Create(testUser).Error
	s.Require().NoError(err)

	settings := &models.NotificationSettings{
		UserID: testUser.ID,
		Provider: models.NotificationProvider{
			Provider: models.NotificationProviderNone,
		},
	}

	err = s.DB.Create(settings).Error
	s.Require().NoError(err)

	provider := models.NotificationProvider{
		Provider: models.NotificationProviderGotify,
		URL:      "https://example.com",
		Token:    "api-token",
	}

	triggers := models.NotificationTriggerOptions{
		DueDate: true,
		PreDue:  true,
	}

	err = s.repo.UpdateNotificationSettings(ctx, testUser.ID, provider, triggers)
	s.Require().NoError(err)

	var updated models.NotificationSettings
	err = s.DB.Where("user_id = ?", testUser.ID).First(&updated).Error
	s.Require().NoError(err)
	s.Equal(models.NotificationProviderGotify, updated.Provider.Provider)
	s.Equal("https://example.com", updated.Provider.URL)
	s.Equal("api-token", updated.Provider.Token)
	s.True(updated.Triggers.DueDate)
	s.True(updated.Triggers.PreDue)
}

func (s *UserTestSuite) TestRequestDeletion() {
	ctx := context.Background()

	user := &models.User{
		DirectoryID: "del-dir",
		ObjectID:    "del-obj",
		CreatedAt:   time.Now(),
	}
	s.Require().NoError(s.DB.Create(user).Error)

	before := time.Now()
	err := s.repo.RequestDeletion(ctx, user.ID)
	s.Require().NoError(err)

	var updated models.User
	s.Require().NoError(s.DB.First(&updated, user.ID).Error)
	s.Require().NotNil(updated.DeletionRequestedAt)
	s.True(updated.DeletionRequestedAt.After(before) || updated.DeletionRequestedAt.Equal(before))
}

func (s *UserTestSuite) TestCancelDeletion() {
	ctx := context.Background()

	now := time.Now()
	user := &models.User{
		DirectoryID:         "cancel-dir",
		ObjectID:            "cancel-obj",
		CreatedAt:           time.Now(),
		DeletionRequestedAt: &now,
	}
	s.Require().NoError(s.DB.Create(user).Error)

	err := s.repo.CancelDeletion(ctx, user.ID)
	s.Require().NoError(err)

	var updated models.User
	s.Require().NoError(s.DB.First(&updated, user.ID).Error)
	s.Nil(updated.DeletionRequestedAt)
}

func (s *UserTestSuite) TestFindUsersForDeletion_ReturnsExpired() {
	ctx := context.Background()

	past := time.Now().Add(-25 * time.Hour)
	user := &models.User{
		DirectoryID:         "expired-dir",
		ObjectID:            "expired-obj",
		CreatedAt:           time.Now(),
		DeletionRequestedAt: &past,
	}
	s.Require().NoError(s.DB.Create(user).Error)

	users, err := s.repo.FindUsersForDeletion(ctx, 24*time.Hour)
	s.Require().NoError(err)
	s.Require().Len(users, 1)
	s.Equal(user.ID, users[0].ID)
}

func (s *UserTestSuite) TestFindUsersForDeletion_ExcludesWithinGracePeriod() {
	ctx := context.Background()

	recent := time.Now().Add(-1 * time.Hour)
	user := &models.User{
		DirectoryID:         "recent-dir",
		ObjectID:            "recent-obj",
		CreatedAt:           time.Now(),
		DeletionRequestedAt: &recent,
	}
	s.Require().NoError(s.DB.Create(user).Error)

	users, err := s.repo.FindUsersForDeletion(ctx, 24*time.Hour)
	s.Require().NoError(err)
	s.Empty(users)
}

func (s *UserTestSuite) TestFindUsersForDeletion_ExcludesNoDeletionRequested() {
	ctx := context.Background()

	user := &models.User{
		DirectoryID: "normal-dir",
		ObjectID:    "normal-obj",
		CreatedAt:   time.Now(),
	}
	s.Require().NoError(s.DB.Create(user).Error)

	users, err := s.repo.FindUsersForDeletion(ctx, 24*time.Hour)
	s.Require().NoError(err)
	s.Empty(users)
}

func (s *UserTestSuite) TestDeleteUser() {
	ctx := context.Background()

	user := &models.User{
		DirectoryID: "todelete-dir",
		ObjectID:    "todelete-obj",
		CreatedAt:   time.Now(),
	}
	s.Require().NoError(s.DB.Create(user).Error)

	err := s.repo.DeleteUser(ctx, user.ID)
	s.Require().NoError(err)

	var count int64
	s.DB.Model(&models.User{}).Where("id = ?", user.ID).Count(&count)
	s.Zero(count)
}

// TestDeleteUser_CascadesAllUserData verifies that deleting a user removes all
// associated data from every user-owned table with no orphaned records left behind.
func (s *UserTestSuite) TestDeleteUser_CascadesAllUserData() {
	ctx := context.Background()

	// Create user with default notification settings via repo so all FKs are consistent.
	user := &models.User{
		DirectoryID: "cascade-dir",
		ObjectID:    "cascade-obj",
		CreatedAt:   time.Now(),
	}
	s.Require().NoError(s.repo.CreateUser(ctx, user))

	// Create a label
	label := &models.Label{
		Name:      "test-label",
		Color:     "#ff0000",
		CreatedBy: user.ID,
	}
	s.Require().NoError(s.DB.Create(label).Error)

	// Create a task linked to that label
	dueDate := time.Now().Add(24 * time.Hour)
	task := &models.Task{
		Title:       "test-task",
		CreatedBy:   user.ID,
		IsActive:    true,
		NextDueDate: &dueDate,
	}
	s.Require().NoError(s.DB.Create(task).Error)

	// Link task to label via task_labels join table
	taskLabel := &models.TaskLabel{TaskID: task.ID, LabelID: label.ID}
	s.Require().NoError(s.DB.Create(taskLabel).Error)

	// Create task history
	completedDate := time.Now()
	history := &models.TaskHistory{
		TaskID:        task.ID,
		CompletedDate: &completedDate,
		DueDate:       &dueDate,
	}
	s.Require().NoError(s.DB.Create(history).Error)

	// Create a notification linked to the task and user
	notification := &models.Notification{
		TaskID:       task.ID,
		UserID:       user.ID,
		Text:         "Task due soon",
		Type:         models.NotificationTypeDueDate,
		ScheduledFor: time.Now().Add(1 * time.Hour),
	}
	s.Require().NoError(s.DB.Create(notification).Error)

	// Verify data exists before deletion
	s.assertRowCount("users", user.ID, 1)
	s.assertRowCount("labels", user.ID, 1)
	s.assertTaskRowCount(task.ID, 1)
	s.assertTaskLabelRowCount(task.ID, 1)
	s.assertTaskHistoryRowCount(task.ID, 1)
	s.assertNotificationUserRowCount(user.ID, 1)
	s.assertNotificationSettingsRowCount(user.ID, 1)

	// Delete the user
	s.Require().NoError(s.repo.DeleteUser(ctx, user.ID))

	// Verify ALL user data is gone
	s.assertRowCount("users", user.ID, 0)
	s.assertRowCount("labels", user.ID, 0)
	s.assertTaskRowCount(task.ID, 0)
	s.assertTaskLabelRowCount(task.ID, 0)
	s.assertTaskHistoryRowCount(task.ID, 0)
	s.assertNotificationUserRowCount(user.ID, 0)
	s.assertNotificationSettingsRowCount(user.ID, 0)
}

func (s *UserTestSuite) assertRowCount(table string, userID int, expected int) {
	var count int64
	column := "id"
	if table == "labels" {
		column = "created_by"
	}
	s.DB.Table(table).Where(column+" = ?", userID).Count(&count)
	s.Equal(int64(expected), count, "expected %d row(s) in %s for user %d", expected, table, userID)
}

func (s *UserTestSuite) assertTaskRowCount(taskID, expected int) {
	var count int64
	s.DB.Model(&models.Task{}).Where("id = ?", taskID).Count(&count)
	s.Equal(int64(expected), count, "expected %d task row(s) for task %d", expected, taskID)
}

func (s *UserTestSuite) assertTaskLabelRowCount(taskID, expected int) {
	var count int64
	s.DB.Model(&models.TaskLabel{}).Where("task_id = ?", taskID).Count(&count)
	s.Equal(int64(expected), count, "expected %d task_label row(s) for task %d", expected, taskID)
}

func (s *UserTestSuite) assertTaskHistoryRowCount(taskID, expected int) {
	var count int64
	s.DB.Model(&models.TaskHistory{}).Where("task_id = ?", taskID).Count(&count)
	s.Equal(int64(expected), count, "expected %d task_history row(s) for task %d", expected, taskID)
}

func (s *UserTestSuite) assertNotificationUserRowCount(userID, expected int) {
	var count int64
	s.DB.Model(&models.Notification{}).Where("user_id = ?", userID).Count(&count)
	s.Equal(int64(expected), count, "expected %d notification row(s) for user %d", expected, userID)
}

func (s *UserTestSuite) assertNotificationSettingsRowCount(userID, expected int) {
	var count int64
	s.DB.Model(&models.NotificationSettings{}).Where("user_id = ?", userID).Count(&count)
	s.Equal(int64(expected), count, "expected %d notification_settings row(s) for user %d", expected, userID)
}
