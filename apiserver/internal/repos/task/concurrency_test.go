package repos

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"taskwiz.app/core/internal/models"
	"taskwiz.app/core/internal/utils/test"
)

type TaskConcurrencyTestSuite struct {
	test.DatabaseTestSuite
	repo     *TaskRepository
	testUser *models.User
}

func TestTaskConcurrencyTestSuite(t *testing.T) {
	suite.Run(t, new(TaskConcurrencyTestSuite))
}

func (s *TaskConcurrencyTestSuite) SetupTest() {
	s.DatabaseTestSuite.SetupTest()
	s.repo = &TaskRepository{db: s.DBPool}
	s.testUser = &models.User{ID: 1, CreatedAt: time.Now()}
	s.Require().NoError(s.DB.Create(s.testUser).Error)
}

func (s *TaskConcurrencyTestSuite) TestConcurrentCreateTask() {
	ctx := context.Background()
	dueDate := time.Now().Add(24 * time.Hour)

	wg := sync.WaitGroup{}
	errCh := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(title string) {
			defer wg.Done()
			task := &models.Task{Title: title, CreatedBy: s.testUser.ID, NextDueDate: &dueDate, IsActive: true, Frequency: models.Frequency{Type: models.RepeatOnce}}
			_, err := s.repo.CreateTask(ctx, task)
			errCh <- err
		}("Task" + string(rune('A'+i)))
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		s.NoError(err)
	}
}
