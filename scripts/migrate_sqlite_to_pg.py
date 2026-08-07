#!/usr/bin/env python3
"""Migrate pve-manage data from SQLite to PostgreSQL.

Prerequisites:
  1. PostgreSQL is running and reachable
  2. Schema migrations have been applied (run `pve-manage migrate -db-url ...`)

Usage:
  python3 migrate_sqlite_to_pg.py --sqlite /path/to/pve-manage.sqlite3 --pg-dsn "postgres://user:pass@host:5432/db?sslmode=disable"
"""

import argparse
import sqlite3
import sys

try:
    import psycopg2
    from psycopg2.extras import execute_values
except ImportError:
    print("psycopg2-binary is required: pip install psycopg2-binary", file=sys.stderr)
    sys.exit(1)

# Insertion order respects logical dependencies (parents before children).
TABLES = [
    "users",
    "security_groups",
    "templates",
    "vms",
    "ssh_keys",
    "node_metrics",
    "vm_tasks",
    "maintenance_tasks",
]

BATCH_SIZE = 5000


def migrate(sqlite_path: str, pg_dsn: str) -> None:
    sconn = sqlite3.connect(sqlite_path)
    sconn.row_factory = sqlite3.Row

    pconn = psycopg2.connect(pg_dsn)
    pconn.autocommit = False

    total_rows = 0
    for table in TABLES:
        # Check if table exists in SQLite
        cur = sconn.execute(
            "SELECT name FROM sqlite_master WHERE type='table' AND name=?", (table,)
        )
        if cur.fetchone() is None:
            print(f"  {table}: not in SQLite, skipping")
            continue

        # Get column names
        cur = sconn.execute(f"SELECT * FROM {table} LIMIT 0")
        columns = [d[0] for d in cur.description]

        # Count rows
        row_count = sconn.execute(f"SELECT COUNT(*) FROM {table}").fetchone()[0]
        if row_count == 0:
            print(f"  {table}: 0 rows, skipping")
            continue

        # Clear any existing data (e.g. from migration test runs)
        pcur = pconn.cursor()
        pcur.execute(f"TRUNCATE TABLE {table} RESTART IDENTITY CASCADE")

        # Copy data in batches
        col_list = ", ".join(columns)
        query = f"INSERT INTO {table} ({col_list}) VALUES %s"
        offset = 0
        copied = 0
        while offset < row_count:
            rows = sconn.execute(
                f"SELECT {col_list} FROM {table} LIMIT {BATCH_SIZE} OFFSET {offset}"
            ).fetchall()
            if not rows:
                break
            data = [tuple(r[col] for col in columns) for r in rows]
            execute_values(pcur, query, data)
            copied += len(data)
            offset += BATCH_SIZE
            pconn.commit()

        # Reset sequence so next insert gets a correct ID
        if "id" in columns:
            pcur.execute(
                f"SELECT setval(pg_get_serial_sequence('{table}', 'id'), "
                f"COALESCE((SELECT MAX(id) FROM {table}), 1))"
            )

        pconn.commit()
        print(f"  {table}: {copied} rows migrated")
        total_rows += copied

    sconn.close()
    pconn.close()
    print(f"\nMigration complete: {total_rows} total rows migrated")


def main():
    parser = argparse.ArgumentParser(description="Migrate SQLite data to PostgreSQL")
    parser.add_argument("--sqlite", required=True, help="Path to SQLite database file")
    parser.add_argument("--pg-dsn", required=True, help="PostgreSQL DSN")
    args = parser.parse_args()
    print(f"Migrating from {args.sqlite} to PostgreSQL...")
    migrate(args.sqlite, args.pg_dsn)


if __name__ == "__main__":
    main()
