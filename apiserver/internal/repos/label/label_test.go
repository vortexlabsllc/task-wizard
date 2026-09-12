package repos

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"taskwiz.app/core/internal/models"
	"taskwiz.app/core/internal/utils/test"
)

type LabelTestSuite struct {
	test.DatabaseTestSuite
	repo     *LabelRepository
	testUser *models.User
}

func TestLabelTestSuite(t *testing.T) {
	suite.Run(t, new(LabelTestSuite))
}

func (s *LabelTestSuite) SetupTest() {
	s.DatabaseTestSuite.SetupTest()
	s.repo = &LabelRepository{db: s.DBPool}

	s.testUser = &models.User{
		ID:        1,
		CreatedAt: time.Now(),
	}

	err := s.DB.Create(s.testUser).Error
	s.Require().NoError(err)
}

func (s *LabelTestSuite) TestGetUserLabels() {
	ctx := context.Background()

	testLabels := []*models.Label{
		{Name: "Work", Color: "#FF0000", CreatedBy: s.testUser.ID},
		{Name: "Personal", Color: "#00FF00", CreatedBy: s.testUser.ID},
	}

	err := s.DB.Create(&testLabels).Error
	s.Require().NoError(err)

	labels, err := s.repo.GetUserLabels(ctx, s.testUser.ID)
	s.Require().NoError(err)
	s.Require().Len(labels, 2)
	s.Equal("Work", labels[0].Name)
	s.Equal("#FF0000", labels[0].Color)
	s.Equal("Personal", labels[1].Name)
	s.Equal("#00FF00", labels[1].Color)
}

func (s *LabelTestSuite) TestCreateLabels() {
	ctx := context.Background()

	testLabels := []*models.Label{
		{Name: "Work", Color: "#FF0000", CreatedBy: s.testUser.ID},
		{Name: "Personal", Color: "#00FF00", CreatedBy: s.testUser.ID},
	}

	err := s.repo.CreateLabels(ctx, testLabels)
	s.Require().NoError(err)

	var count int64
	err = s.DB.Model(&models.Label{}).Where("created_by = ?", s.testUser.ID).Count(&count).Error
	s.Require().NoError(err)
	s.Equal(int64(2), count)
}

func (s *LabelTestSuite) TestLabelExistsByName() {
	ctx := context.Background()

	label := &models.Label{Name: "Work", Color: "#FF0000", CreatedBy: s.testUser.ID}
	err := s.DB.Create(label).Error
	s.Require().NoError(err)

	exists, err := s.repo.LabelExistsByName(ctx, s.testUser.ID, "Work", 0)
	s.Require().NoError(err)
	s.True(exists)

	exists, err = s.repo.LabelExistsByName(ctx, s.testUser.ID, "Nonexistent", 0)
	s.Require().NoError(err)
	s.False(exists)

	exists, err = s.repo.LabelExistsByName(ctx, s.testUser.ID, "Work", label.ID)
	s.Require().NoError(err)
	s.False(exists)

	anotherUser := &models.User{}
	err = s.DB.Create(anotherUser).Error
	s.Require().NoError(err)

	exists, err = s.repo.LabelExistsByName(ctx, anotherUser.ID, "Work", 0)
	s.Require().NoError(err)
	s.False(exists)
}

func (s *LabelTestSuite) TestAreLabelsAssignableByUser() {
	ctx := context.Background()

	testLabels := []*models.Label{
		{Name: "Work", Color: "#FF0000", CreatedBy: s.testUser.ID},
		{Name: "Personal", Color: "#00FF00", CreatedBy: s.testUser.ID},
	}

	err := s.DB.Create(&testLabels).Error
	s.Require().NoError(err)

	anotherUser := &models.User{}
	err = s.DB.Create(anotherUser).Error
	s.Require().NoError(err)

	otherLabel := &models.Label{Name: "Other", Color: "#0000FF", CreatedBy: anotherUser.ID}
	err = s.DB.Create(otherLabel).Error
	s.Require().NoError(err)

	labelIDs := []int{testLabels[0].ID, testLabels[1].ID}
	result := s.repo.AreLabelsAssignableByUser(ctx, s.testUser.ID, labelIDs)
	s.True(result)

	labelIDs = []int{testLabels[0].ID, otherLabel.ID}
	result = s.repo.AreLabelsAssignableByUser(ctx, s.testUser.ID, labelIDs)
	s.False(result)
}

func (s *LabelTestSuite) TestAssignLabelsToTask() {
	ctx := context.Background()

	testLabels := []*models.Label{
		{Name: "Work", Color: "#FF0000", CreatedBy: s.testUser.ID},
		{Name: "Personal", Color: "#00FF00", CreatedBy: s.testUser.ID},
	}

	err := s.DB.Create(&testLabels).Error
	s.Require().NoError(err)

	task := &models.Task{
		Title:     "Test Task",
		CreatedBy: s.testUser.ID,
	}

	err = s.DB.Create(task).Error
	s.Require().NoError(err)

	labelIDs := []int{testLabels[0].ID, testLabels[1].ID}
	err = s.repo.AssignLabelsToTask(ctx, task.ID, s.testUser.ID, labelIDs)
	s.Require().NoError(err)

	var count int64
	err = s.DB.Model(&models.TaskLabel{}).Where("task_id = ?", task.ID).Count(&count).Error
	s.Require().NoError(err)
	s.Equal(int64(2), count)
}

func (s *LabelTestSuite) TestDeleteLabel() {
	ctx := context.Background()

	testLabel := &models.Label{
		Name:      "Work",
		Color:     "#FF0000",
		CreatedBy: s.testUser.ID,
	}

	err := s.DB.Create(testLabel).Error
	s.Require().NoError(err)

	err = s.repo.DeleteLabel(ctx, s.testUser.ID, testLabel.ID)
	s.Require().NoError(err)

	var count int64
	err = s.DB.Model(&models.Label{}).Where("id = ?", testLabel.ID).Count(&count).Error
	s.Require().NoError(err)
	s.Equal(int64(0), count)

	anotherUser := &models.User{}
	err = s.DB.Create(anotherUser).Error
	s.Require().NoError(err)

	otherLabel := &models.Label{Name: "Other", Color: "#0000FF", CreatedBy: anotherUser.ID}
	err = s.DB.Create(otherLabel).Error
	s.Require().NoError(err)

	err = s.repo.DeleteLabel(ctx, s.testUser.ID, otherLabel.ID)
	s.Error(err)
}

func (s *LabelTestSuite) TestUpdateLabel() {
	ctx := context.Background()

	testLabel := &models.Label{
		Name:      "Work",
		Color:     "#FF0000",
		CreatedBy: s.testUser.ID,
	}

	err := s.DB.Create(testLabel).Error
	s.Require().NoError(err)

	testLabel.Name = "Updated Work"
	testLabel.Color = "#FF5500"

	err = s.repo.UpdateLabel(ctx, s.testUser.ID, testLabel)
	s.Require().NoError(err)

	var updatedLabel models.Label
	err = s.DB.First(&updatedLabel, testLabel.ID).Error
	s.Require().NoError(err)
	s.Equal("Updated Work", updatedLabel.Name)
	s.Equal("#FF5500", updatedLabel.Color)
}
