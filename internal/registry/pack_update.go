package registry

import (
	"fmt"

	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/packregistry"
)

// This receipt survives index rebuilds. It distinguishes an interrupted swap
// from an interruption after SQLite committed the new index.
const updateReceipt = "CREATE TABLE IF NOT EXISTS pack_update_receipt (singleton INTEGER PRIMARY KEY CHECK(singleton=1), journal_id TEXT NOT NULL)"

func (r *Registry) UpdatePacks(cfg *config.Config, root string, changes []packregistry.Update) error {
	tx, err := r.beginRebuild()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // Commit closes the transaction.
	return packregistry.ApplyUpdates(root, changes, func(id string) error {
		result, err := rebuild(tx, cfg, RefreshOptions{Root: root, DevMode: true, updatingPacks: true})
		if err != nil {
			return err
		}
		if len(result.Errors) > 0 {
			return fmt.Errorf("reindexing after update: %s", FormatErrors(result.Errors))
		}
		if _, err = tx.Exec(updateReceipt); err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT OR REPLACE INTO pack_update_receipt VALUES(1,?)", id); err != nil {
			return err
		}
		return tx.Commit()
	})
}

func (r *Registry) RecoverPackUpdate(root string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // Commit closes the transaction.
	if _, err = tx.Exec(updateReceipt); err != nil {
		return err
	}
	// Take the same SQLite writer lock as update and refresh before inspecting files.
	if _, err = tx.Exec("UPDATE pack_update_receipt SET journal_id=journal_id WHERE singleton=1"); err != nil {
		return err
	}
	var id string
	if err = tx.QueryRow("SELECT COALESCE(MAX(journal_id),'') FROM pack_update_receipt").Scan(&id); err != nil {
		return err
	}
	if err = packregistry.RecoverUpdate(root, id); err != nil {
		return err
	}
	return tx.Commit()
}
