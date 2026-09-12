package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

func init() {
	Register(&InitialSchemaMigration{})
}

type InitialSchemaMigration struct{}

func (m *InitialSchemaMigration) Version() int {
	return 1
}

func (m *InitialSchemaMigration) Name() string {
	return "initial_schema"
}

func (m *InitialSchemaMigration) Up(ctx context.Context, db *gorm.DB) error {
	var statements []string

	switch db.Name() {
	case "sqlite":
		statements = []string{
			`CREATE TABLE IF NOT EXISTS users (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				display_name TEXT NOT NULL DEFAULT '',
				email TEXT NOT NULL DEFAULT '' UNIQUE,
				password TEXT NOT NULL DEFAULT '',
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME DEFAULT NULL,
				disabled BOOLEAN DEFAULT false
			)`,
			`CREATE TABLE IF NOT EXISTS user_password_resets (
				user_id INTEGER NOT NULL PRIMARY KEY,
				email TEXT NOT NULL,
				token TEXT NOT NULL,
				expiration_date DATETIME NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS app_tokens (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				user_id INTEGER NOT NULL,
				name TEXT NOT NULL,
				token TEXT NOT NULL,
				scopes TEXT,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				expires_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				CONSTRAINT fk_users_app_tokens FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
			)`,
			`CREATE INDEX IF NOT EXISTS idx_app_tokens_token ON app_tokens(token)`,
			`CREATE TABLE IF NOT EXISTS labels (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL,
				color VARCHAR(7) NOT NULL,
				created_by INTEGER NOT NULL,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME DEFAULT NULL,
				CONSTRAINT fk_users_labels FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE
			)`,
			`CREATE INDEX IF NOT EXISTS idx_labels_created_by ON labels(created_by)`,
			`CREATE TABLE IF NOT EXISTS tasks (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				title TEXT NOT NULL,
				frequency_type VARCHAR(9),
				frequency_on VARCHAR(18) DEFAULT NULL,
				frequency_every INTEGER DEFAULT NULL,
				frequency_unit VARCHAR(9) DEFAULT NULL,
				frequency_days TEXT,
				frequency_months TEXT,
				next_due_date DATETIME,
				end_date DATETIME DEFAULT NULL,
				is_rolling BOOLEAN DEFAULT false,
				created_by INTEGER NOT NULL,
				is_active BOOLEAN DEFAULT true,
				notification_enabled BOOLEAN DEFAULT false,
				notification_due_date BOOLEAN DEFAULT false,
				notification_pre_due BOOLEAN DEFAULT false,
				notification_overdue BOOLEAN DEFAULT false,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME DEFAULT NULL,
				CONSTRAINT fk_users_tasks FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE
			)`,
			`CREATE INDEX IF NOT EXISTS idx_tasks_next_due_date ON tasks(next_due_date)`,
			`CREATE INDEX IF NOT EXISTS idx_tasks_created_by ON tasks(created_by)`,
			`CREATE INDEX IF NOT EXISTS idx_tasks_is_active ON tasks(is_active)`,
			`CREATE TABLE IF NOT EXISTS task_labels (
				task_id INTEGER NOT NULL,
				label_id INTEGER NOT NULL,
				PRIMARY KEY (task_id, label_id),
				CONSTRAINT fk_task_labels_task FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
				CONSTRAINT fk_task_labels_label FOREIGN KEY (label_id) REFERENCES labels(id) ON DELETE CASCADE
			)`,
			`CREATE TABLE IF NOT EXISTS task_histories (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				task_id INTEGER NOT NULL,
				completed_date DATETIME,
				due_date DATETIME,
				CONSTRAINT fk_tasks_history FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
			)`,
			`CREATE INDEX IF NOT EXISTS idx_task_histories_task_id ON task_histories(task_id)`,
			`CREATE TABLE IF NOT EXISTS notification_settings (
				user_id INTEGER NOT NULL,
				notifications_provider_type VARCHAR(7),
				notifications_provider_url TEXT,
				notifications_provider_method VARCHAR(4),
				notifications_provider_token TEXT,
				notifications_triggers_enabled BOOLEAN DEFAULT false,
				notifications_triggers_due_date BOOLEAN DEFAULT false,
				notifications_triggers_pre_due BOOLEAN DEFAULT false,
				notifications_triggers_overdue BOOLEAN DEFAULT false,
				CONSTRAINT fk_users_notification_settings FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
			)`,
			`CREATE TABLE IF NOT EXISTS notifications (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				task_id INTEGER NOT NULL,
				user_id INTEGER NOT NULL,
				text TEXT NOT NULL,
				type VARCHAR(8) NOT NULL,
				is_sent BOOLEAN DEFAULT false,
				scheduled_for DATETIME NOT NULL,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				CONSTRAINT fk_tasks_notifications FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
				CONSTRAINT fk_users_notifications FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
			)`,
			`CREATE INDEX IF NOT EXISTS idx_notifications_task_id ON notifications(task_id)`,
			`CREATE INDEX IF NOT EXISTS idx_notifications_user_id ON notifications(user_id)`,
			`CREATE INDEX IF NOT EXISTS idx_notifications_is_sent ON notifications(is_sent)`,
			`CREATE INDEX IF NOT EXISTS idx_notifications_scheduled_for ON notifications(scheduled_for)`,
		}
	case "postgres":
		statements = []string{
			`CREATE TABLE IF NOT EXISTS users (
				id BIGSERIAL PRIMARY KEY,
				display_name TEXT NOT NULL DEFAULT '',
				email TEXT NOT NULL DEFAULT '' UNIQUE,
				password TEXT NOT NULL DEFAULT '',
				created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMPTZ DEFAULT NULL,
				disabled BOOLEAN DEFAULT false
			)`,
			`CREATE TABLE IF NOT EXISTS user_password_resets (
				user_id BIGINT NOT NULL PRIMARY KEY,
				email TEXT NOT NULL,
				token TEXT NOT NULL,
				expiration_date TIMESTAMPTZ NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS app_tokens (
				id BIGSERIAL PRIMARY KEY,
				user_id BIGINT NOT NULL,
				name TEXT NOT NULL,
				token TEXT NOT NULL,
				scopes TEXT,
				created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
				expires_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
				CONSTRAINT fk_users_app_tokens FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
			)`,
			`CREATE INDEX idx_app_tokens_token ON app_tokens(token)`,
			`CREATE TABLE IF NOT EXISTS labels (
				id BIGSERIAL PRIMARY KEY,
				name TEXT NOT NULL,
				color VARCHAR(7) NOT NULL,
				created_by BIGINT NOT NULL,
				created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMPTZ DEFAULT NULL,
				CONSTRAINT fk_users_labels FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE
			)`,
			`CREATE INDEX idx_labels_created_by ON labels(created_by)`,
			`CREATE TABLE IF NOT EXISTS tasks (
				id BIGSERIAL PRIMARY KEY,
				title TEXT NOT NULL,
				frequency_type VARCHAR(9),
				frequency_on VARCHAR(18) DEFAULT NULL,
				frequency_every INTEGER DEFAULT NULL,
				frequency_unit VARCHAR(9) DEFAULT NULL,
				frequency_days TEXT,
				frequency_months TEXT,
				next_due_date TIMESTAMPTZ,
				end_date TIMESTAMPTZ DEFAULT NULL,
				is_rolling BOOLEAN DEFAULT false,
				created_by BIGINT NOT NULL,
				is_active BOOLEAN DEFAULT true,
				notification_enabled BOOLEAN DEFAULT false,
				notification_due_date BOOLEAN DEFAULT false,
				notification_pre_due BOOLEAN DEFAULT false,
				notification_overdue BOOLEAN DEFAULT false,
				created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMPTZ DEFAULT NULL,
				CONSTRAINT fk_users_tasks FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE
			)`,
			`CREATE INDEX idx_tasks_next_due_date ON tasks(next_due_date)`,
			`CREATE INDEX idx_tasks_created_by ON tasks(created_by)`,
			`CREATE INDEX idx_tasks_is_active ON tasks(is_active)`,
			`CREATE TABLE IF NOT EXISTS task_labels (
				task_id BIGINT NOT NULL,
				label_id BIGINT NOT NULL,
				PRIMARY KEY (task_id, label_id),
				CONSTRAINT fk_task_labels_task FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
				CONSTRAINT fk_task_labels_label FOREIGN KEY (label_id) REFERENCES labels(id) ON DELETE CASCADE
			)`,
			`CREATE TABLE IF NOT EXISTS task_histories (
				id BIGSERIAL PRIMARY KEY,
				task_id BIGINT NOT NULL,
				completed_date TIMESTAMPTZ,
				due_date TIMESTAMPTZ,
				CONSTRAINT fk_tasks_history FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
			)`,
			`CREATE INDEX idx_task_histories_task_id ON task_histories(task_id)`,
			`CREATE TABLE IF NOT EXISTS notification_settings (
				user_id BIGINT NOT NULL,
				notifications_provider_type VARCHAR(7),
				notifications_provider_url TEXT,
				notifications_provider_method VARCHAR(4),
				notifications_provider_token TEXT,
				notifications_triggers_enabled BOOLEAN DEFAULT false,
				notifications_triggers_due_date BOOLEAN DEFAULT false,
				notifications_triggers_pre_due BOOLEAN DEFAULT false,
				notifications_triggers_overdue BOOLEAN DEFAULT false,
				CONSTRAINT fk_users_notification_settings FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
			)`,
			`CREATE TABLE IF NOT EXISTS notifications (
				id BIGSERIAL PRIMARY KEY,
				task_id BIGINT NOT NULL,
				user_id BIGINT NOT NULL,
				text TEXT NOT NULL,
				type VARCHAR(8) NOT NULL,
				is_sent BOOLEAN DEFAULT false,
				scheduled_for TIMESTAMPTZ NOT NULL,
				created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
				CONSTRAINT fk_tasks_notifications FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
				CONSTRAINT fk_users_notifications FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
			)`,
			`CREATE INDEX idx_notifications_task_id ON notifications(task_id)`,
			`CREATE INDEX idx_notifications_user_id ON notifications(user_id)`,
			`CREATE INDEX idx_notifications_is_sent ON notifications(is_sent)`,
			`CREATE INDEX idx_notifications_scheduled_for ON notifications(scheduled_for)`,
		}
	default:
		return fmt.Errorf("unsupported dialect: %s", db.Name())
	}

	for _, stmt := range statements {
		if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
			return err
		}
	}

	return nil
}

func (m *InitialSchemaMigration) Down(ctx context.Context, db *gorm.DB) error {
	tables := []string{
		"notifications",
		"notification_settings",
		"task_histories",
		"task_labels",
		"tasks",
		"labels",
		"app_tokens",
		"user_password_resets",
		"users",
	}

	for _, table := range tables {
		if err := db.WithContext(ctx).Exec("DROP TABLE IF EXISTS " + table).Error; err != nil {
			return err
		}
	}

	return nil
}
