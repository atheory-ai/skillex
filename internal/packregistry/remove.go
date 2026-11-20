package packregistry

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/atheory-ai/skillex/internal/filelock"

	"github.com/atheory-ai/skillex/internal/verify"
	"golang.org/x/mod/semver"
)

const removalDir = ".removing"

// A staged removal retains its signed directory name so an interrupted operation
// can be resumed. Hidden directories are excluded from installed pack discovery.
func removalStage(parent *os.Root, create bool) (*os.Root, error) {
	if create {
		if err := parent.Mkdir(removalDir, 0o700); err != nil && !os.IsExist(err) {
			return nil, err
		}
	}
	info, err := parent.Lstat(removalDir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("unsafe pack removal staging directory")
	}
	return parent.OpenRoot(removalDir)
}

// SelectInstalled resolves only local installations and authenticates the selected
// version before the caller opens the project index.
func SelectInstalled(root, selector string) (Entry, error) {
	name, version, exact := strings.Cut(selector, "@")
	if !safeName.MatchString(name) || (exact && !semver.IsValid("v"+version)) {
		return Entry{}, fmt.Errorf("invalid pack selector %q: use name or name@version", selector)
	}
	parent, err := installationParent(root, false)
	if os.IsNotExist(err) {
		return Entry{}, fmt.Errorf("pack is not installed: %s", selector)
	}
	if err != nil {
		return Entry{}, err
	}
	defer parent.Close()
	locations := []*os.Root{parent}
	stage, err := removalStage(parent, false)
	if err == nil {
		defer stage.Close()
		locations = append(locations, stage)
	} else if !os.IsNotExist(err) {
		return Entry{}, err
	}

	var matches []string
	var location *os.Root
	for _, dir := range locations {
		entries, err := fs.ReadDir(dir.FS(), ".")
		if err != nil {
			return Entry{}, err
		}
		for _, e := range entries {
			n, _, ok := strings.Cut(e.Name(), "@")
			if ok && n == name && (!exact || e.Name() == selector) {
				matches = append(matches, e.Name())
				location = dir
			}
		}
	}
	switch len(matches) {
	case 0:
		return Entry{}, fmt.Errorf("pack is not installed: %s", selector)
	case 1:
		lock, err := validateChildWithVerifier(location, matches[0], verify.Manifest)
		return lock.Entry, err
	default:
		slices.Sort(matches)
		return Entry{}, fmt.Errorf("ambiguous installation for %s: %s", selector, strings.Join(matches, ", "))
	}
}

// Remove stages the selected directory, authenticates the staged identity and
// bytes, and lets the caller commit the new index before discarding the files.
// The caller holds the index writer lock through staging and reindexing.
func Remove(root string, selected Entry, reindex func() error) error {
	if err := validateEntry(selected); err != nil {
		return err
	}
	return withMutationLock(root, false, func(parent *os.Root) error {
		if err := noPendingUpdate(parent); err != nil {
			return err
		}
		return removeSelected(parent, selected, reindex)
	})
}

func removeSelected(parent *os.Root, selected Entry, reindex func() error) error {
	id := identity(selected)
	stage, err := removalStage(parent, false)
	if err != nil {
		return err
	}
	defer stage.Close()
	stagedPath := filepath.Join(removalDir, id)
	_, err = stage.Lstat(id)
	if os.IsNotExist(err) {
		if err := parent.Rename(id, stagedPath); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if _, err := parent.Lstat(id); !os.IsNotExist(err) {
		return fmt.Errorf("cannot resume removal of %s while its install path is occupied", id)
	}
	restore := func(cause error) error {
		if err := parent.Rename(stagedPath, id); err != nil {
			return fmt.Errorf("%w; restoring %s from %s: %v", cause, id, stagedPath, err)
		}
		return cause
	}
	lock, err := validateChildWithVerifier(stage, id, verify.Manifest)
	if err != nil {
		return restore(err)
	}
	if lock.Entry != selected {
		return restore(fmt.Errorf("selected pack changed before removal: %s", id))
	}
	if err := reindex(); err != nil {
		return restore(err)
	}
	if err := stage.RemoveAll(id); err != nil {
		return fmt.Errorf("removed %s from index, cleaning staged files: %w", id, err)
	}
	return nil
}

func mutationLock(stage *os.Root) (*os.File, error) {
	lease, err := stage.OpenFile(".lock", os.O_CREATE|os.O_RDWR|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, err
	}
	stat, err := lease.Stat()
	if err != nil {
		lease.Close()
		return nil, err
	}
	if !stat.Mode().IsRegular() {
		lease.Close()
		return nil, fmt.Errorf("unsafe pack removal lock")
	}
	if err := filelock.TryExclusive(lease); err != nil {
		lease.Close()
		return nil, fmt.Errorf("another pack removal may be finishing: %w", err)
	}
	return lease, nil
}
