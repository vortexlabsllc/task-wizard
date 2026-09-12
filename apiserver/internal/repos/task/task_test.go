package repos

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"gorm.io/gorm"
	"taskwiz.app/core/internal/models"
	"taskwiz.app/core/internal/utils/test"
)

type TaskTestSuite struct {
	test.DatabaseTestSuite
	repo     *TaskRepository
	testUser *models.User
}

func TestTaskTestSuite(t *testing.T) {
	suite.Run(t, new(TaskTestSuite))
}

func (s *TaskTestSuite) SetupTest() {
	s.DatabaseTestSuite.SetupTest()
	s.repo = &TaskRepository{db: s.DBPool}

	s.testUser = &models.User{
		ID:        1,
		CreatedAt: time.Now(),
	}

	err := s.DB.Create(s.testUser).Error
	s.Require().NoError(err)
}

func (s *TaskTestSuite) TestCreateTask() {
	ctx := context.Background()
	dueDate := time.Now().Add(24 * time.Hour)

	task := &models.Task{
		Title:       "Test Task",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &dueDate,
		IsRolling:   false,
		IsActive:    true,
		Frequency: models.Frequency{
			Type: models.RepeatOnce,
		},
	}

	id, err := s.repo.CreateTask(ctx, task)
	s.Require().NoError(err)
	s.Require().Greater(id, 0)

	var savedTask models.Task
	err = s.DB.First(&savedTask, id).Error
	s.Require().NoError(err)
	s.Equal("Test Task", savedTask.Title)
	s.Equal(s.testUser.ID, savedTask.CreatedBy)
	s.WithinDuration(*savedTask.NextDueDate, dueDate, time.Second)
	s.Equal(false, savedTask.IsRolling)
	s.Equal(true, savedTask.IsActive)
	s.Equal(string(models.RepeatOnce), string(savedTask.Frequency.Type))
}

func (s *TaskTestSuite) TestCreateTaskWithEndDate() {
	ctx := context.Background()
	dueDate := time.Now().Add(24 * time.Hour)
	endDate := time.Now().Add(30 * 24 * time.Hour) // 30 days from now

	task := &models.Task{
		Title:       "Recurring Task with End Date",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &dueDate,
		EndDate:     &endDate,
		IsRolling:   false,
		IsActive:    true,
		Frequency: models.Frequency{
			Type: models.RepeatWeekly,
		},
	}

	id, err := s.repo.CreateTask(ctx, task)
	s.Require().NoError(err)
	s.Require().Greater(id, 0)

	var savedTask models.Task
	err = s.DB.First(&savedTask, id).Error
	s.Require().NoError(err)
	s.Equal("Recurring Task with End Date", savedTask.Title)
	s.Equal(s.testUser.ID, savedTask.CreatedBy)
	s.WithinDuration(*savedTask.NextDueDate, dueDate, time.Second)
	s.WithinDuration(*savedTask.EndDate, endDate, time.Second)
	s.Equal(false, savedTask.IsRolling)
	s.Equal(true, savedTask.IsActive)
	s.Equal(string(models.RepeatWeekly), string(savedTask.Frequency.Type))
}

func (s *TaskTestSuite) TestUpsertTask() {
	ctx := context.Background()
	dueDate := time.Now().Add(24 * time.Hour)

	task := &models.Task{
		Title:       "Test Task",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &dueDate,
		IsRolling:   false,
		IsActive:    true,
		Frequency: models.Frequency{
			Type: models.RepeatOnce,
		},
	}

	// Create
	err := s.repo.UpsertTask(ctx, task)
	s.Require().NoError(err)
	s.Require().Greater(task.ID, 0)

	// Update
	task.Title = "Updated Test Task"
	err = s.repo.UpsertTask(ctx, task)
	s.Require().NoError(err)

	var updatedTask models.Task
	err = s.DB.First(&updatedTask, task.ID).Error
	s.Require().NoError(err)
	s.Equal("Updated Test Task", updatedTask.Title)
}

func (s *TaskTestSuite) TestGetTask() {
	ctx := context.Background()
	dueDate := time.Now().Add(24 * time.Hour)

	task := &models.Task{
		Title:       "Test Task",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &dueDate,
		IsRolling:   false,
		IsActive:    true,
		Frequency: models.Frequency{
			Type: models.RepeatOnce,
		},
	}

	err := s.DB.Create(task).Error
	s.Require().NoError(err)

	// Create a label and associate it with the task
	label := &models.Label{
		Name:      "Test Label",
		Color:     "#FF0000",
		CreatedBy: s.testUser.ID,
	}
	err = s.DB.Create(label).Error
	s.Require().NoError(err)

	err = s.DB.Exec("INSERT INTO task_labels (task_id, label_id) VALUES (?, ?)", task.ID, label.ID).Error
	s.Require().NoError(err)

	// Test retrieval with labels preloaded
	retrievedTask, err := s.repo.GetTask(ctx, task.ID)
	s.Require().NoError(err)
	s.Equal(task.ID, retrievedTask.ID)
	s.Equal("Test Task", retrievedTask.Title)
	s.Equal(s.testUser.ID, retrievedTask.CreatedBy)
	s.WithinDuration(*retrievedTask.NextDueDate, dueDate, time.Second)

	s.Require().Len(retrievedTask.Labels, 1)
	s.Equal("Test Label", retrievedTask.Labels[0].Name)
	s.Equal("#FF0000", retrievedTask.Labels[0].Color)
}

func (s *TaskTestSuite) TestGetTasks() {
	ctx := context.Background()

	// Create multiple tasks
	dueDate1 := time.Now().Add(24 * time.Hour)
	dueDate2 := time.Now().Add(48 * time.Hour)

	tasks := []*models.Task{
		{
			Title:       "Task 1",
			CreatedBy:   s.testUser.ID,
			NextDueDate: &dueDate1,
			IsActive:    true,
			Frequency: models.Frequency{
				Type: models.RepeatOnce,
			},
		},
		{
			Title:       "Task 2",
			CreatedBy:   s.testUser.ID,
			NextDueDate: &dueDate2,
			IsActive:    true,
			Frequency: models.Frequency{
				Type: models.RepeatWeekly,
			},
		},
		{
			ID:          30,
			Title:       "Inactive Task",
			CreatedBy:   s.testUser.ID,
			NextDueDate: &dueDate1,
			IsActive:    true,
			Frequency: models.Frequency{
				Type: models.RepeatOnce,
			},
		},
	}

	for _, task := range tasks {
		err := s.DB.Create(task).Error
		s.Require().NoError(err)
	}

	// Mark task 30 as inactive
	err := s.DB.Model(&models.Task{}).Where("id = ?", 30).Update("is_active", false).Error
	s.Require().NoError(err)

	// Create another user with their own tasks
	anotherUser := &models.User{}

	err = s.DB.Create(anotherUser).Error
	s.Require().NoError(err)

	otherTask := &models.Task{
		Title:       "Other Task",
		CreatedBy:   anotherUser.ID,
		NextDueDate: &dueDate1,
		IsActive:    true,
	}
	err = s.DB.Create(otherTask).Error
	s.Require().NoError(err)

	// Test retrieval - should only get active tasks for test user
	retrievedTasks, err := s.repo.GetTasks(ctx, s.testUser.ID)
	s.Require().NoError(err)
	s.Require().Len(retrievedTasks, 2)

	// Should be ordered by due date
	s.Equal("Task 1", retrievedTasks[0].Title)
	s.Equal("Task 2", retrievedTasks[1].Title)
}

func (s *TaskTestSuite) TestDeleteTask() {
	ctx := context.Background()
	dueDate := time.Now().Add(24 * time.Hour)

	task := &models.Task{
		Title:       "Test Task",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &dueDate,
		IsActive:    true,
	}

	err := s.DB.Create(task).Error
	s.Require().NoError(err)

	// Delete the task
	err = s.repo.DeleteTask(ctx, task.ID)
	s.Require().NoError(err)

	// Verify task is deleted
	var count int64
	err = s.DB.Model(&models.Task{}).Where("id = ?", task.ID).Count(&count).Error
	s.Require().NoError(err)
	s.Equal(int64(0), count)
}

func (s *TaskTestSuite) TestIsTaskOwner() {
	ctx := context.Background()
	dueDate := time.Now().Add(24 * time.Hour)

	task := &models.Task{
		Title:       "Test Task",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &dueDate,
		IsActive:    true,
	}

	err := s.DB.Create(task).Error
	s.Require().NoError(err)

	// Test with correct owner
	err = s.repo.IsTaskOwner(ctx, task.ID, s.testUser.ID)
	s.Require().NoError(err)

	// Test with incorrect owner
	anotherUser := &models.User{}
	err = s.DB.Create(anotherUser).Error
	s.Require().NoError(err)

	err = s.repo.IsTaskOwner(ctx, task.ID, anotherUser.ID)
	s.Error(err)
}

func (s *TaskTestSuite) TestCompleteTask() {
	ctx := context.Background()
	dueDate := time.Now().Add(24 * time.Hour)
	completedDate := time.Now()

	// Create a non-recurring task
	task := &models.Task{
		Title:       "One-time Task",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &dueDate,
		IsActive:    true,
		Frequency: models.Frequency{
			Type: models.RepeatOnce,
		},
	}

	err := s.DB.Create(task).Error
	s.Require().NoError(err)

	// Complete the one-time task
	err = s.repo.CompleteTask(ctx, task, s.testUser.ID, nil, &completedDate)
	s.Require().NoError(err)

	// Check task is now inactive
	var updatedTask models.Task
	err = s.DB.First(&updatedTask, task.ID).Error
	s.Require().NoError(err)
	s.Equal(false, updatedTask.IsActive)

	// Check task history was created
	var history models.TaskHistory
	err = s.DB.Where("task_id = ?", task.ID).First(&history).Error
	s.Require().NoError(err)
	s.Equal(task.ID, history.TaskID)
	s.WithinDuration(*history.CompletedDate, completedDate, time.Second)
	s.WithinDuration(*history.DueDate, dueDate, time.Second)

	// Create a recurring task
	nextDueDate := time.Now().Add(7 * 24 * time.Hour)
	recurringTask := &models.Task{
		Title:       "Weekly Task",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &dueDate,
		IsActive:    true,
		Frequency: models.Frequency{
			Type: models.RepeatWeekly,
		},
	}

	err = s.DB.Create(recurringTask).Error
	s.Require().NoError(err)

	// Complete the recurring task with a new due date
	err = s.repo.CompleteTask(ctx, recurringTask, s.testUser.ID, &nextDueDate, &completedDate)
	s.Require().NoError(err)

	// Check task is still active with new due date
	var updatedRecurringTask models.Task
	err = s.DB.First(&updatedRecurringTask, recurringTask.ID).Error
	s.Require().NoError(err)
	s.Equal(true, updatedRecurringTask.IsActive)
	s.WithinDuration(*updatedRecurringTask.NextDueDate, nextDueDate, time.Second)
}

func (s *TaskTestSuite) TestRevertActivity() {
	ctx := context.Background()
	dueDate := time.Now().Add(24 * time.Hour)
	completedDate := time.Now()

	task := &models.Task{
		Title:       "Undo Task",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &dueDate,
		IsActive:    true,
		Frequency: models.Frequency{
			Type: models.RepeatOnce,
		},
	}

	err := s.DB.Create(task).Error
	s.Require().NoError(err)

	err = s.repo.CompleteTask(ctx, task, s.testUser.ID, nil, &completedDate)
	s.Require().NoError(err)

	var history models.TaskHistory
	err = s.DB.Where("task_id = ?", task.ID).First(&history).Error
	s.Require().NoError(err)

	err = s.repo.RevertActivity(ctx, task.ID, history.ID)
	s.Require().NoError(err)

	var updatedTask models.Task
	err = s.DB.First(&updatedTask, task.ID).Error
	s.Require().NoError(err)
	s.True(updatedTask.IsActive)
	s.WithinDuration(dueDate, *updatedTask.NextDueDate, time.Second)

	var count int64
	s.DB.Model(&models.TaskHistory{}).Where("task_id = ?", task.ID).Count(&count)
	s.Equal(int64(0), count)
}

func (s *TaskTestSuite) TestRevertActivityRejectsStaleEntry() {
	ctx := context.Background()
	dueDate := time.Now().Add(24 * time.Hour)
	completedDate := time.Now()

	task := &models.Task{
		Title:       "Recurring Undo Task",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &dueDate,
		IsActive:    true,
		Frequency: models.Frequency{
			Type: models.RepeatDaily,
		},
	}

	err := s.DB.Create(task).Error
	s.Require().NoError(err)

	nextDueDate := dueDate.Add(24 * time.Hour)
	err = s.repo.CompleteTask(ctx, task, s.testUser.ID, &nextDueDate, &completedDate)
	s.Require().NoError(err)

	var firstHistory models.TaskHistory
	err = s.DB.Where("task_id = ?", task.ID).Order("id asc").First(&firstHistory).Error
	s.Require().NoError(err)

	// A second completion makes the first history entry stale.
	laterDueDate := nextDueDate.Add(24 * time.Hour)
	err = s.repo.CompleteTask(ctx, task, s.testUser.ID, &laterDueDate, &completedDate)
	s.Require().NoError(err)

	err = s.repo.RevertActivity(ctx, task.ID, firstHistory.ID)
	s.Require().ErrorIs(err, ErrActivityNotLatest)

	// Both history rows remain.
	var count int64
	s.DB.Model(&models.TaskHistory{}).Where("task_id = ?", task.ID).Count(&count)
	s.Equal(int64(2), count)
}

func (s *TaskTestSuite) TestRevertActivityDefaultsToLatest() {
	ctx := context.Background()
	dueDate := time.Now().Add(24 * time.Hour)
	completedDate := time.Now()

	task := &models.Task{
		Title:       "Default Latest Undo Task",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &dueDate,
		IsActive:    true,
		Frequency: models.Frequency{
			Type: models.RepeatDaily,
		},
	}

	err := s.DB.Create(task).Error
	s.Require().NoError(err)

	nextDueDate := dueDate.Add(24 * time.Hour)
	err = s.repo.CompleteTask(ctx, task, s.testUser.ID, &nextDueDate, &completedDate)
	s.Require().NoError(err)

	laterDueDate := nextDueDate.Add(24 * time.Hour)
	err = s.repo.CompleteTask(ctx, task, s.testUser.ID, &laterDueDate, &completedDate)
	s.Require().NoError(err)

	// A non-positive history id reverts the most recent action.
	err = s.repo.RevertActivity(ctx, task.ID, 0)
	s.Require().NoError(err)

	var count int64
	s.DB.Model(&models.TaskHistory{}).Where("task_id = ?", task.ID).Count(&count)
	s.Equal(int64(1), count)
}

func (s *TaskTestSuite) TestRevertActivityNoHistory() {
	ctx := context.Background()

	task := &models.Task{
		Title:     "No History Task",
		CreatedBy: s.testUser.ID,
		IsActive:  true,
		Frequency: models.Frequency{Type: models.RepeatOnce},
	}
	err := s.DB.Create(task).Error
	s.Require().NoError(err)

	err = s.repo.RevertActivity(ctx, task.ID, 0)
	s.Require().ErrorIs(err, gorm.ErrRecordNotFound)
}

func (s *TaskTestSuite) TestRevertActivityRejectsWrongTask() {
	ctx := context.Background()
	completedDate := time.Now()

	task := &models.Task{
		Title:     "Owned Task",
		CreatedBy: s.testUser.ID,
		IsActive:  true,
		Frequency: models.Frequency{Type: models.RepeatOnce},
	}
	err := s.DB.Create(task).Error
	s.Require().NoError(err)

	err = s.repo.CompleteTask(ctx, task, s.testUser.ID, nil, &completedDate)
	s.Require().NoError(err)

	var history models.TaskHistory
	err = s.DB.Where("task_id = ?", task.ID).First(&history).Error
	s.Require().NoError(err)

	// The history id does not belong to task id 99999.
	err = s.repo.RevertActivity(ctx, 99999, history.ID)
	s.Require().Error(err)
}

func (s *TaskTestSuite) TestGetTaskHistory() {
	ctx := context.Background()
	dueDate := time.Now().Add(-24 * time.Hour) // Due yesterday
	completedDate := time.Now()

	// Create a task
	task := &models.Task{
		Title:       "Test Task",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &dueDate,
		IsActive:    true,
	}

	err := s.DB.Create(task).Error
	s.Require().NoError(err)

	// Create multiple task history records
	histories := []models.TaskHistory{
		{
			TaskID:        task.ID,
			DueDate:       &dueDate,
			CompletedDate: &completedDate,
		},
		{
			TaskID:        task.ID,
			DueDate:       &completedDate, // Using completed date as due date for second entry
			CompletedDate: &completedDate,
		},
	}

	for _, history := range histories {
		err := s.DB.Create(&history).Error
		s.Require().NoError(err)
	}

	// Test retrieval
	retrievedHistories, err := s.repo.GetTaskHistory(ctx, task.ID)
	s.Require().NoError(err)
	s.Require().Len(retrievedHistories, 2)

	// Check sorting by due date desc (most recent first)
	s.WithinDuration(*retrievedHistories[0].DueDate, completedDate, time.Second)
	s.WithinDuration(*retrievedHistories[1].DueDate, dueDate, time.Second)
}

func (s *TaskTestSuite) TestScheduleNextDueDate() {
	now := time.Now()

	testCases := []struct {
		name          string
		task          *models.Task
		completedDate time.Time
		expectedType  string
		expectedDelta time.Duration
		expectError   bool
	}{
		{
			name: "Once frequency",
			task: &models.Task{
				NextDueDate: &now,
				Frequency: models.Frequency{
					Type: models.RepeatOnce,
				},
			},
			completedDate: now,
			expectedType:  "nil",
		},
		{
			name: "Daily frequency",
			task: &models.Task{
				NextDueDate: &now,
				Frequency: models.Frequency{
					Type: models.RepeatDaily,
				},
			},
			completedDate: now,
			expectedType:  "time",
			expectedDelta: 24 * time.Hour,
		},
		{
			name: "Weekly frequency",
			task: &models.Task{
				NextDueDate: &now,
				Frequency: models.Frequency{
					Type: models.RepeatWeekly,
				},
			},
			completedDate: now,
			expectedType:  "time",
			expectedDelta: 7 * 24 * time.Hour,
		},
		{
			name: "Monthly frequency",
			task: &models.Task{
				NextDueDate: &now,
				Frequency: models.Frequency{
					Type: models.RepeatMonthly,
				},
			},
			completedDate: now,
			expectedType:  "time",
			// Cannot easily assert exact date due to month length variations
		},
		{
			name: "Yearly frequency",
			task: &models.Task{
				NextDueDate: &now,
				Frequency: models.Frequency{
					Type: models.RepeatYearly,
				},
			},
			completedDate: now,
			expectedType:  "time",
			// Cannot easily assert exact date due to leap year variations
		},
		{
			name: "Custom interval - days",
			task: &models.Task{
				NextDueDate: &now,
				Frequency: models.Frequency{
					Type:  models.RepeatCustom,
					On:    models.Interval,
					Every: 3,
					Unit:  models.Days,
				},
			},
			completedDate: now,
			expectedType:  "time",
			expectedDelta: 3 * 24 * time.Hour,
		},
		{
			name: "Rolling task",
			task: &models.Task{
				NextDueDate: &now,
				IsRolling:   true,
				Frequency: models.Frequency{
					Type: models.RepeatWeekly,
				},
			},
			completedDate: now.Add(48 * time.Hour), // Completed 2 days later
			expectedType:  "time",
			expectedDelta: 7 * 24 * time.Hour, // Should be 7 days from completion
		},
		{
			name: "End date restriction",
			task: &models.Task{
				NextDueDate: &now,
				EndDate:     &now, // End date is today
				Frequency: models.Frequency{
					Type: models.RepeatDaily,
				},
			},
			completedDate: now,
			expectedType:  "nil", // Should not calculate a next due date
		},
		{
			name: "Nil NextDueDate with non-rolling task",
			task: &models.Task{
				NextDueDate: nil,
				IsRolling:   false,
				Frequency: models.Frequency{
					Type: models.RepeatDaily,
				},
			},
			completedDate: now,
			expectError:   true,
		},
		{
			name: "Nil NextDueDate with rolling task",
			task: &models.Task{
				NextDueDate: nil,
				IsRolling:   true,
				Frequency: models.Frequency{
					Type: models.RepeatDaily,
				},
			},
			completedDate: now,
			expectedType:  "time",
			expectedDelta: 24 * time.Hour,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			nextDueDate, err := ScheduleNextDueDate(tc.task, tc.completedDate)

			if tc.expectError {
				s.Require().Error(err)
				s.Nil(nextDueDate)
			} else if tc.expectedType == "nil" {
				s.Nil(nextDueDate)
				s.Nil(err)
			} else {
				s.Require().NotNil(nextDueDate)
				s.Require().NoError(err)

				if tc.expectedDelta > 0 {
					var baseDate time.Time
					if tc.task.IsRolling {
						baseDate = tc.completedDate
					} else {
						baseDate = *tc.task.NextDueDate
					}

					expectedDate := baseDate.Add(tc.expectedDelta)
					s.WithinDuration(expectedDate, *nextDueDate, time.Second)
				}
			}
		})
	}
}

func (s *TaskTestSuite) TestGetRecentActivity() {
	ctx := context.Background()
	completedDate := time.Now()
	dueDate := time.Now().Add(24 * time.Hour)

	// Non-recurring task completed once.
	taskA := &models.Task{
		Title:       "Task A",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &dueDate,
		IsActive:    true,
		Frequency:   models.Frequency{Type: models.RepeatOnce},
	}
	s.Require().NoError(s.DB.Create(taskA).Error)
	s.Require().NoError(s.repo.CompleteTask(ctx, taskA, s.testUser.ID, nil, &completedDate))

	// Recurring task completed twice.
	taskB := &models.Task{
		Title:       "Task B",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &dueDate,
		IsActive:    true,
		Frequency:   models.Frequency{Type: models.RepeatDaily},
	}
	s.Require().NoError(s.DB.Create(taskB).Error)
	nextB1 := dueDate.Add(24 * time.Hour)
	s.Require().NoError(s.repo.CompleteTask(ctx, taskB, s.testUser.ID, &nextB1, &completedDate))
	taskB.NextDueDate = &nextB1
	nextB2 := nextB1.Add(24 * time.Hour)
	s.Require().NoError(s.repo.CompleteTask(ctx, taskB, s.testUser.ID, &nextB2, &completedDate))

	// Another user's activity must not leak.
	anotherUser := &models.User{}
	s.Require().NoError(s.DB.Create(anotherUser).Error)
	otherTask := &models.Task{Title: "Other", CreatedBy: anotherUser.ID, IsActive: true, Frequency: models.Frequency{Type: models.RepeatOnce}}
	s.Require().NoError(s.DB.Create(otherTask).Error)
	s.Require().NoError(s.repo.CompleteTask(ctx, otherTask, anotherUser.ID, nil, &completedDate))

	entries, err := s.repo.GetRecentActivity(ctx, s.testUser.ID, 0, 20)
	s.Require().NoError(err)
	s.Require().Len(entries, 3)

	// Reverse-chronological (id desc): taskB second, taskB first, taskA.
	s.Equal("Task B", entries[0].TaskTitle)
	s.True(entries[0].IsLatest)
	s.Equal("Task B", entries[1].TaskTitle)
	s.False(entries[1].IsLatest)
	s.Equal("Task A", entries[2].TaskTitle)
	s.True(entries[2].IsLatest)

	// Cursor pagination excludes entries with id >= before_id.
	cursor := entries[1].ID
	older, err := s.repo.GetRecentActivity(ctx, s.testUser.ID, cursor, 20)
	s.Require().NoError(err)
	s.Require().Len(older, 1)
	s.Equal("Task A", older[0].TaskTitle)

	// Limit is respected.
	limited, err := s.repo.GetRecentActivity(ctx, s.testUser.ID, 0, 1)
	s.Require().NoError(err)
	s.Require().Len(limited, 1)
	s.Equal(entries[0].ID, limited[0].ID)
}

func (s *TaskTestSuite) TestGetRecentActivityIncludesSkips() {
	ctx := context.Background()
	dueDate := time.Now().Add(24 * time.Hour)

	task := &models.Task{
		Title:       "Skippable",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &dueDate,
		IsActive:    true,
		Frequency:   models.Frequency{Type: models.RepeatDaily},
	}
	s.Require().NoError(s.DB.Create(task).Error)

	// A skip records history with no completed date.
	nextDueDate := dueDate.Add(24 * time.Hour)
	s.Require().NoError(s.repo.CompleteTask(ctx, task, s.testUser.ID, &nextDueDate, nil))

	entries, err := s.repo.GetRecentActivity(ctx, s.testUser.ID, 0, 20)
	s.Require().NoError(err)
	s.Require().Len(entries, 1)
	s.Nil(entries[0].CompletedDate)
	s.True(entries[0].IsLatest)
}

func (s *TaskTestSuite) TestGetTasksDueBefore() {
	ctx := context.Background()

	now := time.Now().UTC()
	past := now.Add(-48 * time.Hour)
	soon := now.Add(24 * time.Hour)
	later := now.Add(72 * time.Hour)
	cutoff := now.Add(48 * time.Hour)

	tasks := []*models.Task{
		{
			Title:       "Past Task",
			CreatedBy:   s.testUser.ID,
			NextDueDate: &past,
			IsActive:    true,
			Frequency:   models.Frequency{Type: models.RepeatOnce},
		},
		{
			Title:       "Soon Task",
			CreatedBy:   s.testUser.ID,
			NextDueDate: &soon,
			IsActive:    true,
			Frequency:   models.Frequency{Type: models.RepeatOnce},
		},
		{
			Title:       "Later Task",
			CreatedBy:   s.testUser.ID,
			NextDueDate: &later,
			IsActive:    true,
			Frequency:   models.Frequency{Type: models.RepeatOnce},
		},
		{
			ID:          40,
			Title:       "Inactive Before Cutoff",
			CreatedBy:   s.testUser.ID,
			NextDueDate: &soon,
			IsActive:    true,
			Frequency:   models.Frequency{Type: models.RepeatOnce},
		},
	}

	for _, task := range tasks {
		err := s.DB.Create(task).Error
		s.Require().NoError(err)
	}

	err := s.DB.Model(&models.Task{}).Where("id = ?", 40).Update("is_active", false).Error
	s.Require().NoError(err)

	anotherUser := &models.User{}
	err = s.DB.Create(anotherUser).Error
	s.Require().NoError(err)

	otherTask := &models.Task{
		Title:       "Other User Task",
		CreatedBy:   anotherUser.ID,
		NextDueDate: &soon,
		IsActive:    true,
	}
	err = s.DB.Create(otherTask).Error
	s.Require().NoError(err)

	// Create a label and attach to "Soon Task" to verify preloading
	label := &models.Label{Name: "Urgent", Color: "#FF0000", CreatedBy: s.testUser.ID}
	err = s.DB.Create(label).Error
	s.Require().NoError(err)
	err = s.DB.Exec("INSERT INTO task_labels (task_id, label_id) VALUES (?, ?)", tasks[1].ID, label.ID).Error
	s.Require().NoError(err)

	result, err := s.repo.GetTasksDueBefore(ctx, s.testUser.ID, cutoff)
	s.Require().NoError(err)
	s.Require().Len(result, 2)

	// Ordered by next_due_date ASC
	s.Equal("Past Task", result[0].Title)
	s.Equal("Soon Task", result[1].Title)

	// Labels are preloaded
	s.Require().Len(result[1].Labels, 1)
	s.Equal("Urgent", result[1].Labels[0].Name)
}

func (s *TaskTestSuite) TestGetTasksByLabel() {
	ctx := context.Background()

	now := time.Now().UTC()
	due1 := now.Add(48 * time.Hour)
	due2 := now.Add(24 * time.Hour)

	label := &models.Label{Name: "Work", Color: "#00FF00", CreatedBy: s.testUser.ID}
	err := s.DB.Create(label).Error
	s.Require().NoError(err)

	otherLabel := &models.Label{Name: "Personal", Color: "#0000FF", CreatedBy: s.testUser.ID}
	err = s.DB.Create(otherLabel).Error
	s.Require().NoError(err)

	taskWithLabel1 := &models.Task{
		Title:       "Work Task A",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &due1,
		IsActive:    true,
		Frequency:   models.Frequency{Type: models.RepeatOnce},
	}
	err = s.DB.Create(taskWithLabel1).Error
	s.Require().NoError(err)

	taskWithLabel2 := &models.Task{
		Title:       "Work Task B",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &due2,
		IsActive:    true,
		Frequency:   models.Frequency{Type: models.RepeatOnce},
	}
	err = s.DB.Create(taskWithLabel2).Error
	s.Require().NoError(err)

	taskWithOtherLabel := &models.Task{
		Title:       "Personal Task",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &due1,
		IsActive:    true,
		Frequency:   models.Frequency{Type: models.RepeatOnce},
	}
	err = s.DB.Create(taskWithOtherLabel).Error
	s.Require().NoError(err)

	inactiveTask := &models.Task{
		ID:          50,
		Title:       "Inactive Work Task",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &due1,
		IsActive:    true,
		Frequency:   models.Frequency{Type: models.RepeatOnce},
	}
	err = s.DB.Create(inactiveTask).Error
	s.Require().NoError(err)
	err = s.DB.Model(&models.Task{}).Where("id = ?", 50).Update("is_active", false).Error
	s.Require().NoError(err)

	// Assign labels
	err = s.DB.Exec("INSERT INTO task_labels (task_id, label_id) VALUES (?, ?)", taskWithLabel1.ID, label.ID).Error
	s.Require().NoError(err)
	err = s.DB.Exec("INSERT INTO task_labels (task_id, label_id) VALUES (?, ?)", taskWithLabel2.ID, label.ID).Error
	s.Require().NoError(err)
	err = s.DB.Exec("INSERT INTO task_labels (task_id, label_id) VALUES (?, ?)", taskWithOtherLabel.ID, otherLabel.ID).Error
	s.Require().NoError(err)
	err = s.DB.Exec("INSERT INTO task_labels (task_id, label_id) VALUES (?, ?)", inactiveTask.ID, label.ID).Error
	s.Require().NoError(err)

	// Also give taskWithLabel1 a second label to verify all labels preload
	err = s.DB.Exec("INSERT INTO task_labels (task_id, label_id) VALUES (?, ?)", taskWithLabel1.ID, otherLabel.ID).Error
	s.Require().NoError(err)

	// Another user with same label name shouldn't appear
	anotherUser := &models.User{}
	err = s.DB.Create(anotherUser).Error
	s.Require().NoError(err)
	otherUserTask := &models.Task{
		Title:       "Other User Work",
		CreatedBy:   anotherUser.ID,
		NextDueDate: &due1,
		IsActive:    true,
	}
	err = s.DB.Create(otherUserTask).Error
	s.Require().NoError(err)
	err = s.DB.Exec("INSERT INTO task_labels (task_id, label_id) VALUES (?, ?)", otherUserTask.ID, label.ID).Error
	s.Require().NoError(err)

	result, err := s.repo.GetTasksByLabel(ctx, s.testUser.ID, label.ID)
	s.Require().NoError(err)
	s.Require().Len(result, 2)

	// Ordered by next_due_date ASC
	s.Equal("Work Task B", result[0].Title)
	s.Equal("Work Task A", result[1].Title)

	// All labels are preloaded (not just the filtered one)
	s.Require().Len(result[1].Labels, 2)
}

func (s *TaskTestSuite) TestSearchTasksByTitle() {
	ctx := context.Background()

	now := time.Now().UTC()
	due1 := now.Add(24 * time.Hour)
	due2 := now.Add(48 * time.Hour)

	groceries := &models.Task{
		Title:       "Buy groceries",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &due2,
		IsActive:    true,
		Frequency:   models.Frequency{Type: models.RepeatOnce},
	}
	s.Require().NoError(s.DB.Create(groceries).Error)

	grocerySort := &models.Task{
		Title:       "Sort Groceries in pantry",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &due1,
		IsActive:    true,
		Frequency:   models.Frequency{Type: models.RepeatOnce},
	}
	s.Require().NoError(s.DB.Create(grocerySort).Error)

	unrelated := &models.Task{
		Title:       "Walk the dog",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &due1,
		IsActive:    true,
		Frequency:   models.Frequency{Type: models.RepeatOnce},
	}
	s.Require().NoError(s.DB.Create(unrelated).Error)

	inactive := &models.Task{
		ID:          60,
		Title:       "Old groceries list",
		CreatedBy:   s.testUser.ID,
		NextDueDate: &due1,
		IsActive:    true,
		Frequency:   models.Frequency{Type: models.RepeatOnce},
	}
	s.Require().NoError(s.DB.Create(inactive).Error)
	s.Require().NoError(s.DB.Model(&models.Task{}).Where("id = ?", 60).Update("is_active", false).Error)

	otherUser := &models.User{}
	s.Require().NoError(s.DB.Create(otherUser).Error)
	otherUserTask := &models.Task{
		Title:       "Their groceries",
		CreatedBy:   otherUser.ID,
		NextDueDate: &due1,
		IsActive:    true,
		Frequency:   models.Frequency{Type: models.RepeatOnce},
	}
	s.Require().NoError(s.DB.Create(otherUserTask).Error)

	// Case-insensitive, matches substring, only active tasks for this user
	result, err := s.repo.SearchTasksByTitle(ctx, s.testUser.ID, "grocer")
	s.Require().NoError(err)
	s.Require().Len(result, 2)
	s.Equal("Sort Groceries in pantry", result[0].Title)
	s.Equal("Buy groceries", result[1].Title)

	// LIKE wildcards in query are treated literally
	resultLiteral, err := s.repo.SearchTasksByTitle(ctx, s.testUser.ID, "%")
	s.Require().NoError(err)
	s.Require().Len(resultLiteral, 0)
}
