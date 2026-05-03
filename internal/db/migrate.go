package db

import (
	"context"
	"database/sql"
	"fmt"
)

type Migration struct {
	Version int
	Name    string
	SQL     string
}

var migrations = []Migration{
	{
		Version: 1,
		Name:    "initial_schema",
		SQL: `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    email TEXT,
    name TEXT,
    groups_json TEXT NOT NULL,
    is_active INTEGER NOT NULL DEFAULT 1,
    is_admin INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_login_at TEXT
);

CREATE TABLE IF NOT EXISTS security_groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_username TEXT NOT NULL,
    name TEXT NOT NULL,
    rules_json TEXT NOT NULL,
    policy_in TEXT NOT NULL DEFAULT 'ACCEPT',
    policy_out TEXT NOT NULL DEFAULT 'ACCEPT',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(owner_username, name)
);

CREATE TABLE IF NOT EXISTS templates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cluster_key TEXT NOT NULL,
    template_vmid INTEGER NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    os_type TEXT,
    real_status_json TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(cluster_key, template_vmid)
);

CREATE TABLE IF NOT EXISTS vms (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_username TEXT NOT NULL,
    cluster_key TEXT NOT NULL,
    vmid INTEGER NOT NULL,
    vmname TEXT NOT NULL,
    ip TEXT NOT NULL,
    password TEXT NOT NULL,
    sshkeys_json TEXT NOT NULL,
    shared_usernames_json TEXT NOT NULL,
    security_group_name TEXT NOT NULL,
    uestc_restricted INTEGER NOT NULL DEFAULT 0,
    config_json TEXT NOT NULL,
    prefer_status_json TEXT NOT NULL,
    real_status_json TEXT NOT NULL,
    sync_state TEXT NOT NULL,
    sync_error TEXT,
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT,
    delete_requested_at TEXT,
    delete_execute_after TEXT,
    UNIQUE(cluster_key, vmid)
);
`,
	},
	{
		Version: 2,
		Name:    "managed_vm_flag",
		SQL: `
ALTER TABLE vms ADD COLUMN managed INTEGER NOT NULL DEFAULT 1;
`,
	},
	{
		Version: 3,
		Name:    "active_vm_unique_index",
		SQL: `
CREATE TABLE vms_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_username TEXT NOT NULL,
    cluster_key TEXT NOT NULL,
    vmid INTEGER NOT NULL,
    vmname TEXT NOT NULL,
    ip TEXT NOT NULL,
    password TEXT NOT NULL,
    sshkeys_json TEXT NOT NULL,
    shared_usernames_json TEXT NOT NULL,
    security_group_name TEXT NOT NULL,
    uestc_restricted INTEGER NOT NULL DEFAULT 0,
    config_json TEXT NOT NULL,
    prefer_status_json TEXT NOT NULL,
    real_status_json TEXT NOT NULL,
    sync_state TEXT NOT NULL,
    sync_error TEXT,
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT,
    delete_requested_at TEXT,
    delete_execute_after TEXT,
    managed INTEGER NOT NULL DEFAULT 1
);

INSERT INTO vms_new(
    id, owner_username, cluster_key, vmid, vmname, ip, password,
    sshkeys_json, shared_usernames_json, security_group_name, uestc_restricted,
    config_json, prefer_status_json, real_status_json, sync_state, sync_error,
    version, created_at, updated_at, deleted_at, delete_requested_at, delete_execute_after, managed
)
SELECT
    id, owner_username, cluster_key, vmid, vmname, ip, password,
    sshkeys_json, shared_usernames_json, security_group_name, uestc_restricted,
    config_json, prefer_status_json, real_status_json, sync_state, sync_error,
    version, created_at, updated_at, deleted_at, delete_requested_at, delete_execute_after, managed
FROM vms;

DROP TABLE vms;
ALTER TABLE vms_new RENAME TO vms;
CREATE UNIQUE INDEX IF NOT EXISTS idx_vms_active_cluster_vmid
    ON vms(cluster_key, vmid)
    WHERE deleted_at IS NULL;
`,
	},
	{
		Version: 4,
		Name:    "ssh_keys_table",
		SQL: `
CREATE TABLE IF NOT EXISTS ssh_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_username TEXT NOT NULL,
    name TEXT NOT NULL,
    public_key TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(owner_username, name),
    UNIQUE(owner_username, public_key)
);
`,
	},
	{
		Version: 5,
		Name:    "node_column_and_metrics",
		SQL: `
ALTER TABLE vms ADD COLUMN node TEXT;

CREATE TABLE IF NOT EXISTS node_metrics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cluster_key TEXT NOT NULL,
    node TEXT NOT NULL,
    cpu_ratio REAL NOT NULL,
    mem_ratio REAL NOT NULL,
    recorded_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(cluster_key, node, recorded_at)
);

CREATE INDEX IF NOT EXISTS idx_node_metrics_cluster_node_recorded
    ON node_metrics(cluster_key, node, recorded_at);
`,
	},
	{
		Version: 6,
		Name:    "security_group_policy_columns",
		SQL: `
-- policy_in/policy_out were already present in the initial schema.
-- Keep this migration as a no-op so fresh databases and older installs
-- can both advance schema_migrations without trying to add duplicate columns.
SELECT 1;
`,
	},
	{
		Version: 7,
		Name:    "vm_tasks_table",
		SQL: `
CREATE TABLE IF NOT EXISTS vm_tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    vm_id INTEGER NOT NULL,
    seq INTEGER NOT NULL,
    kind TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    status TEXT NOT NULL,
    error TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    UNIQUE(vm_id, seq)
);

CREATE INDEX IF NOT EXISTS idx_vm_tasks_vm_status_seq
    ON vm_tasks(vm_id, status, seq, id);
`,
	},
	{
		Version: 8,
		Name:    "maintenance_tasks_table",
		SQL: `
CREATE TABLE IF NOT EXISTS maintenance_tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    kind TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    status TEXT NOT NULL,
    error TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_maintenance_tasks_kind_status_created
    ON maintenance_tasks(kind, status, created_at, id);
`,
	},
	{
		Version: 9,
		Name:    "vm_task_queue_paused",
		SQL: `
ALTER TABLE vms ADD COLUMN task_queue_paused INTEGER NOT NULL DEFAULT 0;
`,
	},
	{
		Version: 10,
		Name:    "vm_target_node",
		SQL: `
ALTER TABLE vms ADD COLUMN target_node TEXT;
`,
	},
}

func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	);`); err != nil {
		return err
	}

	for _, migration := range migrations {
		applied, err := migrationApplied(ctx, db, migration.Version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d (%s): %w", migration.Version, migration.Name, err)
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations(version, name) VALUES(?, ?)`,
			migration.Version,
			migration.Name,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d (%s): %w", migration.Version, migration.Name, err)
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}

func migrationApplied(ctx context.Context, db *sql.DB, version int) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, version).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}
