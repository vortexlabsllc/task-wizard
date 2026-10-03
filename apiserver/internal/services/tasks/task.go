package tasks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"taskwiz.app/core/internal/models"
	lRepo "taskwiz.app/core/internal/repos/label"
	nRepo "taskwiz.app/core/internal/repos/notifier"
	tRepo "taskwiz.app/core/internal/repos/task"
	"taskwiz.app/core/internal/services/logging"
	"taskwiz.app/core/internal/services/notifications"
	"taskwiz.app/core/internal/telemetry"
	"taskwiz.app/core/internal/ws"
)

type TaskService struct {
	t        *tRepo.TaskRepository
	ws       *ws.WSServer
	notifier *notifications.Notifier
	n        *nRepo.NotificationRepository
	l        *lRepo.LabelRepository
}

func NewTaskService(t *tRepo.TaskRepository, ws *ws.WSServer, notifier *notifications.Notifier, n *nRepo.NotificationRepository, l *lRepo.LabelRepository) *TaskService {
	return &TaskService{
		t:        t,
		ws:       ws,
		notifier: notifier,
		n:        n,
		l:        l,
	}
}

// maxActivityPageSize bounds how many activity entries a single request may return,
// regardless of the limit supplied over HTTP or WebSocket.
const maxActivityPageSize = 20

func (s *TaskService) GetUserTasks(ctx context.Context, userID int) (int, interface{}) {
	log := logging.FromContext(ctx)
	tasks, err := s.t.GetTasks(ctx, userID)
	if err != nil {
		log.Errorf("error getting tasks: %s", err.Error())
		telemetry.TrackError(ctx, "task_get_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error getting tasks",
		}
	}

	return http.StatusOK, gin.H{
		"tasks": tasks,
	}
}

func (s *TaskService) GetTasksDueBefore(ctx context.Context, userID int, before time.Time) (int, interface{}) {
	log := logging.FromContext(ctx)
	tasks, err := s.t.GetTasksDueBefore(ctx, userID, before)
	if err != nil {
		log.Errorf("error getting tasks due before %s: %s", before.String(), err.Error())
		telemetry.TrackError(ctx, "task_get_due_before_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error getting tasks",
		}
	}

	return http.StatusOK, gin.H{
		"tasks": tasks,
	}
}

func (s *TaskService) GetTasksByLabel(ctx context.Context, userID int, labelID int) (int, interface{}) {
	log := logging.FromContext(ctx)
	tasks, err := s.t.GetTasksByLabel(ctx, userID, labelID)
	if err != nil {
		log.Errorf("error getting tasks by label %d: %s", labelID, err.Error())
		telemetry.TrackError(ctx, "task_get_by_label_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error getting tasks",
		}
	}

	return http.StatusOK, gin.H{
		"tasks": tasks,
	}
}

func (s *TaskService) SearchTasksByTitle(ctx context.Context, userID int, query string) (int, interface{}) {
	log := logging.FromContext(ctx)
	tasks, err := s.t.SearchTasksByTitle(ctx, userID, query)
	if err != nil {
		log.Errorf("error searching tasks by title %q: %s", query, err.Error())
		telemetry.TrackError(ctx, "task_search_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error searching tasks",
		}
	}

	return http.StatusOK, gin.H{
		"tasks": tasks,
	}
}

func (s *TaskService) GetRecentActivity(ctx context.Context, userID, beforeID, limit int) (int, interface{}) {
	log := logging.FromContext(ctx)

	if limit <= 0 || limit > maxActivityPageSize {
		limit = maxActivityPageSize
	}
	if beforeID < 0 {
		beforeID = 0
	}

	entries, err := s.t.GetRecentActivity(ctx, userID, beforeID, limit)
	if err != nil {
		log.Errorf("error getting recent activity: %s", err.Error())
		telemetry.TrackError(ctx, "task_get_activity_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error getting recent activity",
		}
	}

	return http.StatusOK, gin.H{
		"activity": entries,
	}
}

func (s *TaskService) GetTask(ctx context.Context, userID, taskID int) (int, interface{}) {
	log := logging.FromContext(ctx)

	task, err := s.t.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return http.StatusNotFound, gin.H{"error": "Task not found"}
		}
		log.Errorf("error getting task: %s", err.Error())
		telemetry.TrackError(ctx, "task_get_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error getting task",
		}
	}

	if userID != task.CreatedBy {
		telemetry.TrackWarning(ctx, "task_not_found", "task-service", "User not allowed to view task", nil)
		return http.StatusNotFound, gin.H{"error": "Task not found"}
	}

	return http.StatusOK, gin.H{
		"task": task,
	}
}

func createShallowLabels(labelIds []int) []models.Label {
	labels := make([]models.Label, len(labelIds))
	for i, id := range labelIds {
		labels[i] = models.Label{ID: id}
	}
	return labels
}

func (s *TaskService) CreateTask(ctx context.Context, userID int, req models.CreateTaskReq) (int, interface{}) {
	log := logging.FromContext(ctx)

	var dueDate *time.Time
	if req.NextDueDate != "" {
		rawDueDate, err := time.Parse(time.RFC3339, req.NextDueDate)
		if err != nil {
			log.Errorf("error parsing due date: %s", err.Error())
			telemetry.TrackError(ctx, "task_create_failed", "task-service", err, nil)
			return http.StatusBadRequest, gin.H{
				"error": "Due date must be in UTC format",
			}
		}

		rawDueDate = rawDueDate.UTC()
		dueDate = &rawDueDate
	}

	var endDate *time.Time
	if req.EndDate != "" {
		rawEndDate, err := time.Parse(time.RFC3339, req.EndDate)
		if err != nil {
			log.Errorf("error parsing end date: %s", err.Error())
			telemetry.TrackError(ctx, "task_create_failed", "task-service", err, nil)
			return http.StatusBadRequest, gin.H{
				"error": "End date must be in UTC format",
			}
		}

		rawEndDate = rawEndDate.UTC()
		endDate = &rawEndDate
	}

	createdTask := &models.Task{
		Title:        req.Title,
		Frequency:    req.Frequency,
		NextDueDate:  dueDate,
		EndDate:      endDate,
		CreatedBy:    userID,
		IsRolling:    req.IsRolling,
		IsActive:     true,
		Notification: req.Notification,
	}

	id, err := s.t.CreateTask(ctx, createdTask)
	createdTask.ID = id

	if err != nil {
		log.Errorf("error creating task: %s", err.Error())
		telemetry.TrackError(ctx, "task_create_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error creating task",
		}
	}

	if err := s.l.AssignLabelsToTask(ctx, createdTask.ID, userID, req.Labels); err != nil {
		log.Errorf("error assigning labels to task: %s", err.Error())
		telemetry.TrackError(ctx, "task_label_assign_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error adding labels",
		}
	}

	createdTask.Labels = createShallowLabels(req.Labels)

	go func(task *models.Task, logger *zap.SugaredLogger) {
		ctx := logging.ContextWithLogger(context.Background(), logger)
		s.n.GenerateNotifications(ctx, task)
	}(createdTask, log)

	s.ws.BroadcastToUser(userID, ws.WSResponse{
		Action: "task_created",
		Data:   createdTask,
	})

	return http.StatusCreated, gin.H{
		"task": id,
	}
}

func (s *TaskService) EditTask(ctx context.Context, userID int, req models.UpdateTaskReq) (int, interface{}) {
	log := logging.FromContext(ctx)

	var dueDate *time.Time
	if req.NextDueDate != "" {
		rawDueDate, err := time.Parse(time.RFC3339, req.NextDueDate)
		if err != nil {
			log.Errorf("error parsing due date: %s", err.Error())
			telemetry.TrackError(ctx, "task_edit_failed", "task-service", err, nil)
			return http.StatusBadRequest, gin.H{
				"error": "Due date must be in UTC format",
			}
		}

		rawDueDate = rawDueDate.UTC()
		dueDate = &rawDueDate
	}

	var endDate *time.Time
	if req.EndDate != "" {
		rawEndDate, err := time.Parse(time.RFC3339, req.EndDate)
		if err != nil {
			log.Errorf("error parsing end date: %s", err.Error())
			telemetry.TrackError(ctx, "task_edit_failed", "task-service", err, nil)
			return http.StatusBadRequest, gin.H{
				"error": "End date must be in UTC format",
			}
		}

		rawEndDate = rawEndDate.UTC()
		endDate = &rawEndDate
	}

	taskId := req.ID
	oldTask, err := s.t.GetTask(ctx, taskId)

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return http.StatusNotFound, gin.H{"error": "Task not found"}
		}
		log.Errorf("error getting task: %s", err.Error())
		telemetry.TrackError(ctx, "task_get_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error getting task",
		}
	}

	if userID != oldTask.CreatedBy {
		telemetry.TrackWarning(ctx, "task_not_found", "task-service", "User not allowed to edit task", nil)
		return http.StatusNotFound, gin.H{"error": "Task not found"}
	}

	if err := s.l.AssignLabelsToTask(ctx, taskId, userID, req.Labels); err != nil {
		log.Errorf("error assigning labels to task: %s", err.Error())
		telemetry.TrackError(ctx, "task_label_assign_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error adding labels",
		}
	}

	updatedTask := &models.Task{
		ID:           taskId,
		Title:        req.Title,
		Frequency:    req.Frequency,
		NextDueDate:  dueDate,
		EndDate:      endDate,
		CreatedBy:    userID,
		IsRolling:    req.IsRolling,
		Notification: req.Notification,
		IsActive:     oldTask.IsActive,
	}

	if err := s.t.UpsertTask(ctx, updatedTask); err != nil {
		log.Errorf("error upserting task: %s", err.Error())
		telemetry.TrackError(ctx, "task_edit_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error upserting task",
		}
	}

	updatedTask.Labels = createShallowLabels(req.Labels)

	go func(task *models.Task, logger *zap.SugaredLogger) {
		ctx := logging.ContextWithLogger(context.Background(), logger)
		s.n.GenerateNotifications(ctx, task)
	}(updatedTask, log)

	s.ws.BroadcastToUser(userID, ws.WSResponse{
		Action: "task_updated",
		Data:   updatedTask,
	})

	return http.StatusNoContent, nil
}

func (s *TaskService) DeleteTask(ctx context.Context, userID, taskID int) (int, interface{}) {
	log := logging.FromContext(ctx)

	if err := s.t.IsTaskOwner(ctx, taskID, userID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			telemetry.TrackWarning(ctx, "task_not_found", "task-service", "User not allowed to delete task", nil)
			return http.StatusNotFound, gin.H{"error": "Task not found"}
		}
		log.Errorf("error checking task ownership: %s", err.Error())
		telemetry.TrackError(ctx, "task_delete_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error deleting task",
		}
	}

	if err := s.t.DeleteTask(ctx, taskID); err != nil {
		log.Errorf("error deleting task: %s", err.Error())
		telemetry.TrackError(ctx, "task_delete_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error deleting task",
		}
	}

	s.ws.BroadcastToUser(userID, ws.WSResponse{
		Action: "task_deleted",
		Data: gin.H{
			"id": taskID,
		},
	})

	return http.StatusNoContent, nil
}

func (s *TaskService) SkipTask(ctx context.Context, userID, taskID int) (int, interface{}) {
	log := logging.FromContext(ctx)
	task, err := s.t.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return http.StatusNotFound, gin.H{"error": "Task not found"}
		}
		log.Errorf("error getting task: %s", err.Error())
		telemetry.TrackError(ctx, "task_get_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error getting task",
		}
	}

	if userID != task.CreatedBy {
		telemetry.TrackWarning(ctx, "task_not_found", "task-service", "User not allowed to skip task", nil)
		return http.StatusNotFound, gin.H{"error": "Task not found"}
	}

	if task.NextDueDate == nil {
		telemetry.TrackWarning(ctx, "task_skip_failed", "task-service", "Task has no due date to skip", nil)
		return http.StatusBadRequest, gin.H{
			"error": "Task has no due date to skip",
		}
	}

	nextDueDate, err := tRepo.ScheduleNextDueDate(task, task.NextDueDate.UTC())
	if err != nil {
		log.Errorf("error scheduling next due date: %s", err.Error())
		telemetry.TrackError(ctx, "task_skip_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error scheduling next due date",
		}
	}

	if err := s.t.CompleteTask(ctx, task, userID, nextDueDate, nil); err != nil {
		log.Errorf("error completing task: %s", err.Error())
		telemetry.TrackError(ctx, "task_skip_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error completing task",
		}
	}

	// Rebuild the post-write task in memory instead of re-reading it (see
	// CompleteTask for why a post-write read is unsafe on a read replica).
	task.NextDueDate = nextDueDate
	if nextDueDate == nil {
		task.IsActive = false
	}

	go func(task *models.Task, logger *zap.SugaredLogger) {
		ctx := logging.ContextWithLogger(context.Background(), logger)
		s.n.GenerateNotifications(ctx, task)
	}(task, log)

	s.ws.BroadcastToUser(userID, ws.WSResponse{
		Action: "task_skipped",
		Data:   task,
	})

	return http.StatusOK, gin.H{
		"task": task,
	}
}

func (s *TaskService) UpdateDueDate(ctx context.Context, userID, taskID int, req models.UpdateDueDateReq) (int, interface{}) {
	log := logging.FromContext(ctx)

	task, err := s.t.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return http.StatusNotFound, gin.H{"error": "Task not found"}
		}
		log.Errorf("error getting task: %s", err.Error())
		telemetry.TrackError(ctx, "task_get_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error getting task",
		}
	}

	if userID != task.CreatedBy {
		telemetry.TrackWarning(ctx, "task_not_found", "task-service", "User not allowed to update due date", nil)
		return http.StatusNotFound, gin.H{"error": "Task not found"}
	}

	if req.DueDate != "" {
		rawDueDate, err := time.Parse(time.RFC3339, req.DueDate)
		if err != nil {
			log.Errorf("error parsing due date: %s", err.Error())
			telemetry.TrackError(ctx, "task_update_due_date_failed", "task-service", err, nil)
			return http.StatusBadRequest, gin.H{
				"error": "Due date must be in UTC format",
			}
		}

		rawDueDate = rawDueDate.UTC()
		task.NextDueDate = &rawDueDate
	}

	if err := s.t.UpsertTask(ctx, task); err != nil {
		log.Errorf("error updating due date: %s", err.Error())
		telemetry.TrackError(ctx, "task_update_due_date_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error updating due date",
		}
	}

	go func(task *models.Task, logger *zap.SugaredLogger) {
		ctx := logging.ContextWithLogger(context.Background(), logger)
		s.n.GenerateNotifications(ctx, task)
	}(task, log)

	s.ws.BroadcastToUser(userID, ws.WSResponse{
		Action: "task_updated",
		Data:   task,
	})

	return http.StatusOK, gin.H{
		"task": task,
	}
}

func (s *TaskService) CompleteTask(ctx context.Context, userID, taskID int, endRecurrence bool) (int, interface{}) {
	log := logging.FromContext(ctx)

	task, err := s.t.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return http.StatusNotFound, gin.H{"error": "Task not found"}
		}
		log.Errorf("error getting task: %s", err.Error())
		telemetry.TrackError(ctx, "task_get_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error getting task",
		}
	}

	if userID != task.CreatedBy {
		telemetry.TrackWarning(ctx, "task_not_found", "task-service", "User not allowed to complete task", nil)
		return http.StatusNotFound, gin.H{"error": "Task not found"}
	}

	completedDate := time.Now().UTC()
	var nextDueDate *time.Time = nil

	if !endRecurrence {
		nextDueDate, err = tRepo.ScheduleNextDueDate(task, completedDate)
		if err != nil {
			log.Errorf("error scheduling next due date: %s", err.Error())
			telemetry.TrackError(ctx, "task_complete_failed", "task-service", err, nil)
			return http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("Error scheduling next due date: %s", err),
			}
		}
	}

	if err := s.t.CompleteTask(ctx, task, userID, nextDueDate, &completedDate); err != nil {
		log.Errorf("error completing task: %s", err.Error())
		telemetry.TrackError(ctx, "task_complete_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error completing task",
		}
	}

	// Rebuild the post-write task in memory instead of re-reading it. A read
	// here would hit the read replica and could return a stale snapshot that
	// makes a just-applied completion appear to be undone.
	task.NextDueDate = nextDueDate
	if nextDueDate == nil {
		task.IsActive = false
	}

	go func(task *models.Task, logger *zap.SugaredLogger) {
		ctx := logging.ContextWithLogger(context.Background(), logger)
		s.n.GenerateNotifications(ctx, task)
	}(task, log)

	s.ws.BroadcastToUser(userID, ws.WSResponse{
		Action: "task_completed",
		Data:   task,
	})

	return http.StatusOK, gin.H{
		"task": task,
	}
}

func (s *TaskService) RevertAction(ctx context.Context, userID, taskID, historyID int) (int, interface{}) {
	log := logging.FromContext(ctx)
	task, err := s.t.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return http.StatusNotFound, gin.H{"error": "Task not found"}
		}
		log.Errorf("error getting task: %s", err.Error())
		telemetry.TrackError(ctx, "task_get_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error getting task",
		}
	}

	if userID != task.CreatedBy {
		telemetry.TrackWarning(ctx, "task_not_found", "task-service", "User not allowed to revert task action", nil)
		return http.StatusNotFound, gin.H{"error": "Task not found"}
	}

	restoredDueDate, err := s.t.RevertActivity(ctx, taskID, historyID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, tRepo.ErrActivityNotLatest) {
			return http.StatusConflict, gin.H{
				"error": "This action can no longer be reverted",
			}
		}

		log.Errorf("error reverting task action: %s", err.Error())
		telemetry.TrackError(ctx, "task_revert_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error reverting task action",
		}
	}

	// Rebuild the post-write task in memory instead of re-reading it (see
	// CompleteTask for why a post-write read is unsafe on a read replica).
	task.NextDueDate = restoredDueDate
	task.IsActive = true

	go func(task *models.Task, logger *zap.SugaredLogger) {
		ctx := logging.ContextWithLogger(context.Background(), logger)
		s.n.GenerateNotifications(ctx, task)
	}(task, log)

	s.ws.BroadcastToUser(userID, ws.WSResponse{
		Action: "task_uncompleted",
		Data:   task,
	})

	return http.StatusOK, gin.H{
		"task": task,
	}
}

func (s *TaskService) GetTaskHistory(ctx context.Context, userID, taskID int) (int, interface{}) {
	log := logging.FromContext(ctx)

	if err := s.t.IsTaskOwner(ctx, taskID, userID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			telemetry.TrackWarning(ctx, "task_not_found", "task-service", "User not allowed to view task history", nil)
			return http.StatusNotFound, gin.H{"error": "Task not found"}
		}
		log.Errorf("error checking task ownership: %s", err.Error())
		telemetry.TrackError(ctx, "task_history_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error getting task history",
		}
	}

	TaskHistory, err := s.t.GetTaskHistory(ctx, taskID)
	if err != nil {
		log.Errorf("error getting task history: %s", err.Error())
		telemetry.TrackError(ctx, "task_history_failed", "task-service", err, nil)
		return http.StatusInternalServerError, gin.H{
			"error": "Error getting task history",
		}
	}

	return http.StatusOK, gin.H{
		"history": TaskHistory,
	}
}
