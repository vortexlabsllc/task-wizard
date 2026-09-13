package repos

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	config "taskwiz.app/core/config"
	"taskwiz.app/core/internal/models"
	database "taskwiz.app/core/internal/utils/database"
)

// Connection routing for this repository:
//   - RW (primary): all writes and transactions
//   - R: ordinary reads
//   - RO: heavy/reporting reads (title search, recent activity)
type TaskRepository struct {
	db *database.DB
}

func NewTaskRepository(db *database.DB, cfg *config.Config) *TaskRepository {
	return &TaskRepository{db: db}
}

func (r *TaskRepository) UpsertTask(c context.Context, task *models.Task) error {
	return r.db.RW().WithContext(c).Model(&task).Save(task).Error
}

func (r *TaskRepository) CreateTask(c context.Context, task *models.Task) (int, error) {
	if err := r.db.RW().WithContext(c).Create(task).Error; err != nil {
		return 0, err
	}
	return task.ID, nil
}

func (r *TaskRepository) GetTask(c context.Context, taskID int) (*models.Task, error) {
	var task models.Task
	if err := r.db.R().WithContext(c).
		Model(&models.Task{}).
		Preload("Labels").
		First(&task, taskID).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *TaskRepository) GetTasks(c context.Context, userID int) ([]*models.Task, error) {
	var tasks []*models.Task

	if err := r.db.R().WithContext(c).
		Where("created_by = ? AND is_active = ?", userID, true).
		Order("next_due_date ASC").
		Preload("Labels").
		Find(&tasks).Error; err != nil {
		return nil, err
	}

	return tasks, nil
}

func (r *TaskRepository) GetTasksDueBefore(c context.Context, userID int, before time.Time) ([]*models.Task, error) {
	var tasks []*models.Task

	if err := r.db.R().WithContext(c).
		Where("created_by = ? AND is_active = ? AND next_due_date < ?", userID, true, before).
		Order("next_due_date ASC").
		Preload("Labels").
		Find(&tasks).Error; err != nil {
		return nil, err
	}

	return tasks, nil
}

func (r *TaskRepository) GetTasksByLabel(c context.Context, userID int, labelID int) ([]*models.Task, error) {
	var tasks []*models.Task

	if err := r.db.R().WithContext(c).
		Where("created_by = ? AND is_active = ?", userID, true).
		Joins("JOIN task_labels ON task_labels.task_id = tasks.id AND task_labels.label_id = ?", labelID).
		Order("next_due_date ASC").
		Preload("Labels").
		Find(&tasks).Error; err != nil {
		return nil, err
	}

	return tasks, nil
}

func (r *TaskRepository) SearchTasksByTitle(c context.Context, userID int, query string) ([]*models.Task, error) {
	var tasks []*models.Task

	// Escape LIKE wildcards so they match literally. Use '!' as the escape
	// character because backslash has dialect-specific meaning inside string
	// literals in some databases.
	escaped := strings.ReplaceAll(query, "!", "!!")
	escaped = strings.ReplaceAll(escaped, "%", "!%")
	escaped = strings.ReplaceAll(escaped, "_", "!_")
	pattern := "%" + strings.ToLower(escaped) + "%"

	if err := r.db.RO().WithContext(c).
		Where("created_by = ? AND is_active = ? AND LOWER(title) LIKE ? ESCAPE '!'", userID, true, pattern).
		Order("next_due_date ASC").
		Preload("Labels").
		Find(&tasks).Error; err != nil {
		return nil, err
	}

	return tasks, nil
}

func (r *TaskRepository) GetRecentActivity(c context.Context, userID, beforeID, limit int) ([]*models.ActivityEntry, error) {
	var entries []*models.ActivityEntry

	q := r.db.RO().WithContext(c).
		Table("task_histories AS th").
		Select(`th.id AS id, th.task_id AS task_id, t.title AS task_title,
			th.completed_date AS completed_date, th.due_date AS due_date,
			CASE WHEN th.id = (SELECT MAX(th2.id) FROM task_histories th2 WHERE th2.task_id = th.task_id) THEN 1 ELSE 0 END AS is_latest`).
		Joins("JOIN tasks t ON t.id = th.task_id").
		Where("t.created_by = ?", userID)

	if beforeID > 0 {
		q = q.Where("th.id < ?", beforeID)
	}

	if err := q.Order("th.id DESC").Limit(limit).Scan(&entries).Error; err != nil {
		return nil, err
	}

	return entries, nil
}

func (r *TaskRepository) DeleteTask(c context.Context, id int) error {
	return r.db.RW().WithContext(c).Where("id = ?", id).Delete(&models.Task{}).Error
}

func (r *TaskRepository) IsTaskOwner(c context.Context, taskID int, userID int) error {
	var task models.Task
	return r.db.R().WithContext(c).Model(&models.Task{}).Where("id = ? AND created_by = ?", taskID, userID).First(&task).Error
}

func (r *TaskRepository) CompleteTask(c context.Context, task *models.Task, userID int, dueDate *time.Time, completedDate *time.Time) error {
	err := r.db.RW().WithContext(c).Transaction(func(tx *gorm.DB) error {
		ch := &models.TaskHistory{
			TaskID:        task.ID,
			CompletedDate: completedDate,
			DueDate:       task.NextDueDate,
		}
		if err := tx.Create(ch).Error; err != nil {
			return err
		}
		updates := map[string]interface{}{}
		updates["next_due_date"] = dueDate

		if dueDate == nil {
			updates["is_active"] = false
		}

		if err := tx.Model(&models.Task{}).Where("id = ?", task.ID).Updates(updates).Error; err != nil {
			return err
		}

		return nil
	})

	return err
}

// ErrActivityNotLatest indicates a revert was attempted on a history entry that is
// no longer the most recent action for the task.
var ErrActivityNotLatest = errors.New("history entry is not the latest action for the task")

func (r *TaskRepository) RevertActivity(c context.Context, taskID int, historyID int) error {
	return r.db.RW().WithContext(c).Transaction(func(tx *gorm.DB) error {
		var latestID *int
		if err := tx.Model(&models.TaskHistory{}).
			Where("task_id = ?", taskID).
			Select("MAX(id)").
			Scan(&latestID).Error; err != nil {
			return err
		}

		if latestID == nil {
			return gorm.ErrRecordNotFound
		}

		// A non-positive historyID means "revert the latest action", letting
		// callers undo without first resolving the history entry themselves.
		if historyID <= 0 {
			historyID = *latestID
		}

		if historyID != *latestID {
			return ErrActivityNotLatest
		}

		var entry models.TaskHistory
		if err := tx.Where("id = ? AND task_id = ?", historyID, taskID).First(&entry).Error; err != nil {
			return err
		}

		if err := tx.Delete(&entry).Error; err != nil {
			return err
		}

		updates := map[string]interface{}{
			"next_due_date": entry.DueDate,
			"is_active":     true,
		}

		return tx.Model(&models.Task{}).Where("id = ?", taskID).Updates(updates).Error
	})
}

func (r *TaskRepository) GetTaskHistory(c context.Context, taskID int) ([]*models.TaskHistory, error) {
	var histories []*models.TaskHistory
	if err := r.db.R().WithContext(c).Where("task_id = ?", taskID).Order("due_date desc").Find(&histories).Error; err != nil {
		return nil, err
	}
	return histories, nil
}

func ScheduleNextDueDate(task *models.Task, completedDate time.Time) (*time.Time, error) {
	var freq = task.Frequency
	if freq.Type == "once" {
		return nil, nil
	}

	var baseDate time.Time
	if task.IsRolling {
		baseDate = completedDate
	} else {
		if task.NextDueDate == nil {
			return nil, errors.New("task has no next due date")
		}
		baseDate = *task.NextDueDate
	}

	if baseDate.IsZero() {
		return nil, errors.New("unable to calculate next due date")
	}

	var nextDueDate time.Time
	if freq.Type == "daily" {
		nextDueDate = baseDate.AddDate(0, 0, 1)
	} else if freq.Type == "weekly" {
		nextDueDate = baseDate.AddDate(0, 0, 7)
	} else if freq.Type == "monthly" {
		nextDueDate = baseDate.AddDate(0, 1, 0)
	} else if freq.Type == "yearly" {
		nextDueDate = baseDate.AddDate(1, 0, 0)
	} else if freq.Type == "custom" {
		if freq.On == "interval" {
			switch freq.Unit {
			case "hours":
				nextDueDate = baseDate.Add(time.Duration(freq.Every) * time.Hour)
			case "days":
				nextDueDate = baseDate.AddDate(0, 0, freq.Every)
			case "weeks":
				nextDueDate = baseDate.AddDate(0, 0, 7*freq.Every)
			case "months":
				nextDueDate = baseDate.AddDate(0, freq.Every, 0)
			case "years":
				nextDueDate = baseDate.AddDate(freq.Every, 0, 0)
			}
		} else if freq.On == "days_of_the_week" {
			currentWeekDay := int32(baseDate.Weekday())
			days := freq.Days

			if len(days) == 0 {
				return nil, errors.New("days of the week cannot be empty")
			}

			duringThisWeek := false
			for _, day := range days {
				if day > currentWeekDay {
					duringThisWeek = true
					nextDueDate = baseDate.AddDate(0, 0, int(day-currentWeekDay))
					break
				}
			}

			if !duringThisWeek {
				daysUntilNextWeek := 7 - int(currentWeekDay)
				nextDueDate = baseDate.AddDate(0, 0, daysUntilNextWeek+int(days[0]))
			}
		} else if freq.On == "day_of_the_months" {
			currentMonth := int32(baseDate.Month())
			months := freq.Months

			if len(months) == 0 {
				return nil, errors.New("months cannot be empty")
			}

			duringThisYear := false
			for _, month := range months {
				if month > currentMonth {
					duringThisYear = true
					nextDueDate = baseDate.AddDate(0, int(month-currentMonth), 0)
					break
				}
			}

			if !duringThisYear {
				monthsUntilNextYear := 12 - int(currentMonth)
				nextDueDate = baseDate.AddDate(0, monthsUntilNextYear+int(months[0]), 0)
			}
		}
	}

	if task.EndDate != nil && nextDueDate.After(*task.EndDate) {
		return nil, nil
	}

	return &nextDueDate, nil
}
