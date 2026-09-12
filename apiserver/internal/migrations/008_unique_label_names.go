package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

func init() {
	Register(&UniqueLabelNamesMigration{})
}

type UniqueLabelNamesMigration struct{}

func (m *UniqueLabelNamesMigration) Version() int {
	return 8
}

func (m *UniqueLabelNamesMigration) Name() string {
	return "unique_label_names"
}

func (m *UniqueLabelNamesMigration) Up(ctx context.Context, db *gorm.DB) error {
	dbCtx := db.WithContext(ctx)

	// Deduplicate labels per (created_by, name), keeping the lowest ID:
	//  1. reassign task_labels from duplicate labels to the kept label,
	//     skipping rows that would collide with an existing pair
	//  2. remove task_labels pointing to duplicate labels
	//  3. delete the duplicate labels themselves
	var dedup []string
	switch db.Name() {
	case "postgres":
		dedup = []string{
			`INSERT INTO task_labels (task_id, label_id)
			SELECT tl.task_id, keep.id
			FROM task_labels tl
			JOIN labels dup ON tl.label_id = dup.id
			JOIN (
				SELECT MIN(id) AS id, name, created_by
				FROM labels
				GROUP BY created_by, name
			) keep ON dup.created_by = keep.created_by AND dup.name = keep.name
			WHERE dup.id != keep.id
			ON CONFLICT DO NOTHING`,

			`DELETE FROM task_labels WHERE label_id IN (
				SELECT l.id FROM labels l
				JOIN (
					SELECT MIN(id) AS keep_id, name, created_by
					FROM labels
					GROUP BY created_by, name
				) k ON l.created_by = k.created_by AND l.name = k.name
				WHERE l.id != k.keep_id
			)`,

			`DELETE FROM labels WHERE id NOT IN (
				SELECT MIN(id) FROM labels GROUP BY created_by, name
			)`,
		}
	default: // sqlite
		dedup = []string{
			`INSERT OR IGNORE INTO task_labels (task_id, label_id)
			SELECT tl.task_id, keep.id
			FROM task_labels tl
			JOIN labels dup ON tl.label_id = dup.id
			JOIN (
				SELECT MIN(id) AS id, name, created_by
				FROM labels
				GROUP BY created_by, name
			) keep ON dup.created_by = keep.created_by AND dup.name = keep.name
			WHERE dup.id != keep.id`,

			`DELETE FROM task_labels WHERE label_id IN (
				SELECT l.id FROM labels l
				JOIN (
					SELECT MIN(id) AS keep_id, name, created_by
					FROM labels
					GROUP BY created_by, name
				) k ON l.created_by = k.created_by AND l.name = k.name
				WHERE l.id != k.keep_id
			)`,

			`DELETE FROM labels WHERE id NOT IN (
				SELECT MIN(id) FROM labels GROUP BY created_by, name
			)`,
		}
	}

	for _, stmt := range dedup {
		if err := dbCtx.Exec(stmt).Error; err != nil {
			return fmt.Errorf("failed to deduplicate labels: %w", err)
		}
	}

	return dbCtx.Exec("CREATE UNIQUE INDEX idx_labels_created_by_name ON labels(created_by, name)").Error
}

func (m *UniqueLabelNamesMigration) Down(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Exec("DROP INDEX IF EXISTS idx_labels_created_by_name").Error
}
