package migrations

import (
	"context"

	"gorm.io/gorm"
)

func init() {
	Register(&TaskHistoriesTaskIDIDIndexMigration{})
}

type TaskHistoriesTaskIDIDIndexMigration struct{}

func (m *TaskHistoriesTaskIDIDIndexMigration) Version() int {
	return 9
}

func (m *TaskHistoriesTaskIDIDIndexMigration) Name() string {
	return "task_histories_task_id_id_index"
}

func (m *TaskHistoriesTaskIDIDIndexMigration) Up(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Exec("CREATE INDEX IF NOT EXISTS idx_task_histories_task_id_id ON task_histories(task_id, id)").Error
}

func (m *TaskHistoriesTaskIDIDIndexMigration) Down(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Exec("DROP INDEX IF EXISTS idx_task_histories_task_id_id").Error
}
