package repos

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	config "taskwiz.app/core/config"
	"taskwiz.app/core/internal/models"
	database "taskwiz.app/core/internal/utils/database"
)

// Connection routing for this repository:
//   - RW (primary): all writes and transactions
//   - R: ordinary reads
type LabelRepository struct {
	db *database.DB
}

func NewLabelRepository(db *database.DB, cfg *config.Config) *LabelRepository {
	return &LabelRepository{db: db}
}

func (r *LabelRepository) GetUserLabels(ctx context.Context, userID int) ([]*models.Label, error) {
	var labels []*models.Label
	if err := r.db.R().WithContext(ctx).Select("id", "name", "color").Where("created_by = ?", userID).Find(&labels).Error; err != nil {
		return nil, err
	}
	return labels, nil
}

func (r *LabelRepository) CreateLabels(ctx context.Context, labels []*models.Label) error {
	return r.db.RW().WithContext(ctx).Create(&labels).Error
}

func (r *LabelRepository) LabelExistsByName(ctx context.Context, userID int, name string, excludeLabelID int) (bool, error) {
	var count int64
	q := r.db.R().WithContext(ctx).Model(&models.Label{}).Where("created_by = ? AND name = ?", userID, name)
	if excludeLabelID > 0 {
		q = q.Where("id != ?", excludeLabelID)
	}
	if err := q.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *LabelRepository) AreLabelsAssignableByUser(ctx context.Context, userID int, labels []int) bool {
	var count int64

	if err := r.db.R().WithContext(ctx).Model(&models.Label{}).Where("id IN (?) AND created_by = ?", labels, userID).Count(&count).Error; err != nil {
		return false
	}

	return count == int64(len(labels))
}

func (r *LabelRepository) AssignLabelsToTask(ctx context.Context, taskID int, userID int, labels []int) error {
	if len(labels) < 1 {
		return nil
	}

	if !r.AreLabelsAssignableByUser(ctx, userID, labels) {
		return errors.New("labels are not assignable by user")
	}

	var taskLabels []*models.TaskLabel
	for _, labelID := range labels {
		taskLabels = append(taskLabels, &models.TaskLabel{
			TaskID:  taskID,
			LabelID: labelID,
		})
	}

	return r.db.RW().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Where("task_id = ?", taskID).Delete(&models.TaskLabel{}).Error; err != nil {
			return err
		}

		if err := tx.WithContext(ctx).Create(&taskLabels).Error; err != nil {
			return err
		}

		return nil
	})
}

func (r *LabelRepository) DeleteLabel(ctx context.Context, userID int, labelID int) error {
	return r.db.RW().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var labelCount int64
		if err := tx.Model(&models.Label{}).Where("id = ? AND created_by = ?", labelID, userID).Count(&labelCount).Error; err != nil {
			return err
		}

		if labelCount < 1 {
			return errors.New("label is not owned by user")
		}

		if err := tx.Where("id = ?", labelID).Delete(&models.Label{}).Error; err != nil {
			return fmt.Errorf("error deleting label: %s", err.Error())
		}

		return nil
	})
}

func (r *LabelRepository) UpdateLabel(ctx context.Context, userID int, label *models.Label) error {
	return r.db.RW().WithContext(ctx).Model(&models.Label{}).Where("id = ? and created_by = ?", label.ID, userID).Updates(label).Error
}
