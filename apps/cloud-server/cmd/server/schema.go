package main

import (
	"context"
	"database/sql"
)

// ensureSimulatedOrderColumn upgrades databases created before simulated orders
// were added. The base CREATE TABLE migration includes the column for new DBs.
func ensureSimulatedOrderColumn(ctx context.Context, db *sql.DB) error {
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='agent_orders' AND COLUMN_NAME='simulated'`).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err = db.ExecContext(ctx, "ALTER TABLE agent_orders ADD COLUMN simulated BOOLEAN NOT NULL DEFAULT FALSE AFTER detail_status")
	return err
}

func ensureOrderIPColumn(ctx context.Context, db *sql.DB) error {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='agent_orders' AND COLUMN_NAME='order_ip'`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err := db.ExecContext(ctx, "ALTER TABLE agent_orders ADD COLUMN order_ip VARCHAR(45) NOT NULL DEFAULT '' AFTER simulated")
	return err
}
