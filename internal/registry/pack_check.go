package registry

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/packregistry"
)

type rowReader interface{ QueryRow(string, ...any) *sql.Row }

func currentCheck(db rowReader, root string) (*packregistry.CatalogCheck, error) {
	disk, err := packregistry.SavedCheck(root)
	if err != nil || db == nil {
		return disk, err
	}
	var data []byte
	err = db.QueryRow("SELECT evidence FROM pack_catalog_check WHERE singleton=1").Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return disk, nil
	}
	if err != nil {
		return nil, err
	}
	indexed, err := packregistry.DecodeCheck(data)
	if err != nil {
		return nil, err
	}
	// Either copy can be newer after an interrupted check or an index restore.
	if err := indexed.Accepts(disk); err == nil {
		return indexed, nil
	}
	if err := disk.Accepts(indexed); err != nil {
		return nil, err
	}
	return disk, nil
}

// PackStatus is read-only, including in projects that have never built an index.
func PackStatus(root string) ([]packregistry.PackStatus, error) {
	locks, err := packregistry.List(root)
	if err != nil {
		return nil, err
	}
	if len(locks) == 0 {
		return []packregistry.PackStatus{}, nil
	}
	path := filepath.Join(root, ".skillex", "index.db")
	var reader rowReader
	if _, err := os.Stat(path); err == nil {
		uriPath := filepath.ToSlash(path)
		if filepath.VolumeName(path) != "" && !strings.HasPrefix(uriPath, "/") {
			uriPath = "/" + uriPath
		}
		u := url.URL{Scheme: "file", Path: uriPath, RawQuery: "mode=ro"}
		db, err := sql.Open("sqlite", u.String())
		if err != nil {
			return nil, err
		}
		defer db.Close()
		var exists bool
		if err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE name='pack_catalog_check')").Scan(&exists); err != nil {
			return nil, err
		}
		if exists {
			reader = db
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	c, err := currentCheck(reader, root)
	if err != nil {
		return nil, err
	}
	return c.Status(locks), nil
}

func (r *Registry) CheckPacks(cfg *config.Config, root string, c *packregistry.CatalogCheck) ([]packregistry.PackStatus, error) {
	tx, err := r.beginRebuild()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck // Commit closes the transaction.
	previous, err := currentCheck(tx, root)
	if err != nil {
		return nil, err
	}
	if err = c.Accepts(previous); err != nil {
		return nil, err
	}
	var status []packregistry.PackStatus
	err = c.PublishCheck(root, func(locks []packregistry.Lock, data []byte) error {
		if _, err := tx.Exec("INSERT OR REPLACE INTO pack_catalog_check VALUES(1,?)", data); err != nil {
			return err
		}
		result, err := rebuildWithPacks(tx, cfg, RefreshOptions{Root: root, DevMode: true}, locks, c)
		if err != nil {
			return err
		}
		if len(result.Errors) > 0 {
			return fmt.Errorf("reindexing checked packs: %s", FormatErrors(result.Errors))
		}
		status = result.PackStatus
		return tx.Commit()
	})
	return status, err
}

func (r *Registry) TaskDisabled(path string) (bool, error) {
	var disabled bool
	err := r.db.QueryRow("SELECT EXISTS(SELECT 1 FROM pack_disabled_tasks WHERE path=?)", path).Scan(&disabled)
	return disabled, err
}
