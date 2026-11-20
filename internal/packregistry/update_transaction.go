package packregistry

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/atheory-ai/skillex/internal/verify"
	"golang.org/x/mod/semver"
)

const pendingDir = ".updating"
const maxJournalSize = 8 << 20

type replacement struct{ From, To Entry }
type updateJournal struct {
	ID    string
	Packs []replacement
}

func identity(e Entry) string { return e.Name + "@" + e.Version }

func noPendingUpdate(parent *os.Root) error {
	_, err := parent.Lstat(pendingDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("unfinished pack update: run 'skillex pack update --recover'")
}

// HasPendingUpdate probes the existing cache without creating project files.
func HasPendingUpdate(root string) (bool, error) {
	parent, err := installationParent(root, false)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer parent.Close()
	info, err := parent.Lstat(pendingDir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("unsafe pack update journal")
	}
	return true, nil
}

func verifySelected(dir *os.Root, e Entry) error {
	lock, err := validateChildWithVerifier(dir, identity(e), verify.Manifest)
	if err != nil {
		return err
	}
	if lock.Entry != e {
		return fmt.Errorf("installed pack changed: %s", identity(e))
	}
	return nil
}

func validReplacement(p replacement) error {
	if err := validateEntry(p.From); err != nil {
		return err
	}
	if err := validateEntry(p.To); err != nil {
		return err
	}
	if p.From.Name != p.To.Name || semver.Compare("v"+p.From.Version, "v"+p.To.Version) >= 0 {
		return fmt.Errorf("invalid pack replacement")
	}
	return nil
}

func withMutationLock(root string, create bool, action func(*os.Root) error) error {
	parent, err := installationParent(root, create)
	if err != nil {
		return err
	}
	defer parent.Close()
	stage, err := removalStage(parent, true)
	if err != nil {
		return err
	}
	defer stage.Close()
	lease, err := mutationLock(stage)
	if err != nil {
		return err
	}
	defer lease.Close()
	return action(parent)
}

// ApplyUpdates swaps all reviewed packs under the lifecycle lock. The callback
// commits the rebuilt index and this journal ID together in SQLite.
func ApplyUpdates(root string, changes []Update, commit func(string) error) error {
	if len(changes) == 0 {
		return nil
	}
	return withMutationLock(root, false, func(parent *os.Root) error {
		if err := noPendingUpdate(parent); err != nil {
			return err
		}
		journal := updateJournal{ID: rand.Text()}
		temp := ".prepare-" + journal.ID
		if err := parent.Mkdir(temp, 0o700); err != nil {
			return err
		}
		defer parent.RemoveAll(temp) //nolint:errcheck // Unpublished staging files only.
		if err := parent.Mkdir(filepath.Join(temp, "old"), 0o700); err != nil {
			return err
		}
		incomingPath := filepath.Join(temp, "new")
		if err := parent.Mkdir(incomingPath, 0o700); err != nil {
			return err
		}
		incoming, err := parent.OpenRoot(incomingPath)
		if err != nil {
			return err
		}
		err = stageUpdates(parent, incoming, changes, &journal)
		incoming.Close()
		if err != nil {
			return err
		}
		data, err := json.Marshal(journal)
		if err != nil {
			return err
		}
		if len(data) > maxJournalSize {
			return fmt.Errorf("pack update exceeds journal size limit; update a named pack instead")
		}
		if err := writeNew(parent, filepath.Join(temp, "journal.json"), data); err != nil {
			return err
		}
		if err := parent.Rename(temp, pendingDir); err != nil {
			return err
		}

		for _, p := range journal.Packs {
			from, to := identity(p.From), identity(p.To)
			if err = parent.Rename(from, filepath.Join(pendingDir, "old", from)); err != nil {
				break
			}
			if err = parent.Rename(filepath.Join(pendingDir, "new", to), to); err != nil {
				break
			}
		}
		if err == nil {
			err = commit(journal.ID)
		}
		if err != nil {
			return errors.Join(err, finishUpdate(parent, journal, false))
		}
		return finishUpdate(parent, journal, true)
	})
}

func stageUpdates(parent, incoming *os.Root, changes []Update, journal *updateJournal) error {
	for _, change := range changes {
		p := replacement{change.From.Entry, change.To.Lock.Entry}
		if err := validReplacement(p); err != nil {
			return err
		}
		if err := verifySelected(parent, p.From); err != nil {
			return err
		}
		for _, target := range []string{identity(p.To), filepath.Join(removalDir, identity(p.To)), filepath.Join(removalDir, identity(p.From))} {
			if _, err := parent.Lstat(target); err == nil {
				return fmt.Errorf("pack location already occupied: %s", target)
			} else if !os.IsNotExist(err) {
				return err
			}
		}
		files, err := change.To.verifiedFiles()
		if err != nil {
			return err
		}
		if err := change.To.installInto(incoming, files); err != nil {
			return err
		}
		if err := verifySelected(incoming, p.To); err != nil {
			return err
		}
		journal.Packs = append(journal.Packs, p)
	}
	return nil
}

// RecoverUpdate completes cleanup after commit, or restores the old directories
// if SQLite rolled back. It never resolves or downloads another pack.
func RecoverUpdate(root, committedID string) error {
	return withMutationLock(root, false, func(parent *os.Root) error {
		info, err := parent.Lstat(pendingDir)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("unsafe pack update journal")
		}
		data, err := readRegular(parent, filepath.Join(pendingDir, "journal.json"), maxJournalSize)
		if err != nil {
			return err
		}
		var j updateJournal
		if err := json.Unmarshal(data, &j); err != nil {
			return err
		}
		if j.ID == "" || len(j.Packs) == 0 {
			return fmt.Errorf("invalid pack update journal")
		}
		seen := map[string]bool{}
		for _, p := range j.Packs {
			if seen[p.From.Name] {
				return fmt.Errorf("duplicate pack in update journal")
			}
			seen[p.From.Name] = true
			if err := validReplacement(p); err != nil {
				return err
			}
		}
		return finishUpdate(parent, j, j.ID == committedID)
	})
}

func finishUpdate(parent *os.Root, j updateJournal, committed bool) error {
	if err := checkJournalFiles(parent, j); err != nil {
		return fmt.Errorf("checking recovery files: %w", err)
	}
	backups, err := parent.OpenRoot(filepath.Join(pendingDir, "old"))
	if err != nil {
		return err
	}
	err = restoreOrCheck(parent, backups, j, committed)
	backups.Close()
	if err != nil {
		return fmt.Errorf("pack update recovery required: %w", err)
	}
	// Retire the journal atomically before unlinking. A partial cleanup is
	// invisible to the installed catalog and cannot replay a finished update.
	retired := ".finished-" + rand.Text()
	if err := parent.Rename(pendingDir, retired); err != nil {
		return err
	}
	return parent.RemoveAll(retired)
}

func restoreOrCheck(parent, backups *os.Root, j updateJournal, committed bool) error {
	for _, p := range j.Packs {
		from, to := identity(p.From), identity(p.To)
		if committed {
			if err := verifySelected(parent, p.To); err != nil {
				return err
			}
			continue
		}
		_, err := backups.Lstat(from)
		if os.IsNotExist(err) {
			if err := verifySelected(parent, p.From); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if err := verifySelected(backups, p.From); err != nil {
			return err
		}
		if _, err := parent.Lstat(from); !os.IsNotExist(err) {
			return fmt.Errorf("cannot restore occupied pack location %s", from)
		}
		if _, err := parent.Lstat(to); err == nil {
			if err := verifySelected(parent, p.To); err != nil {
				return err
			}
			if err := parent.Rename(to, filepath.Join(pendingDir, "new", to)); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := parent.Rename(filepath.Join(pendingDir, "old", from), from); err != nil {
			return err
		}
	}
	return nil
}

// Only retire the transaction's signed packs. Preserve unexpected files for
// inspection rather than treating everything under a recovery path as disposable.
func checkJournalFiles(parent *os.Root, j updateJournal) error {
	scope, err := parent.OpenRoot(pendingDir)
	if err != nil {
		return err
	}
	defer scope.Close()
	entries, err := fs.ReadDir(scope.FS(), ".")
	if err != nil {
		return err
	}
	if len(entries) != 3 {
		return fmt.Errorf("unexpected update journal contents")
	}
	for _, dir := range entries {
		if dir.Name() == "journal.json" && dir.Type().IsRegular() {
			continue
		}
		if !dir.IsDir() || (dir.Name() != "new" && dir.Name() != "old") {
			return fmt.Errorf("unexpected update journal entry %q", dir.Name())
		}
		expected := make(map[string]Entry, len(j.Packs))
		for _, p := range j.Packs {
			e := p.From
			if dir.Name() == "new" {
				e = p.To
			}
			expected[identity(e)] = e
		}
		stage, err := scope.OpenRoot(dir.Name())
		if err != nil {
			return err
		}
		err = checkStagedEntries(stage, expected)
		stage.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func checkStagedEntries(stage *os.Root, expected map[string]Entry) error {
	children, err := fs.ReadDir(stage.FS(), ".")
	if err != nil {
		return err
	}
	for _, child := range children {
		entry, ok := expected[child.Name()]
		if !ok {
			return fmt.Errorf("unexpected staged pack %q", child.Name())
		}
		if err := verifySelected(stage, entry); err != nil {
			return err
		}
	}
	return nil
}
