// Package watch keeps the skill index current for a long-lived process.
// It watches skill directories that a scan already found, and updates the
// skill that changed. A directory created somewhere else joins the watch
// after skillex refresh changes the registry.
package watch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/atheory-ai/skillex/internal/registry"
	"github.com/atheory-ai/skillex/internal/scanner"
	"github.com/atheory-ai/skillex/internal/validator"
	"github.com/fsnotify/fsnotify"
)

const (
	// DefaultIdleTimeout is how long the MCP server keeps skill watches armed
	// without a request or a file change.
	DefaultIdleTimeout = 10 * time.Minute
	// DefaultDebounce collapses a burst of editor writes into one skill update.
	DefaultDebounce = 250 * time.Millisecond
)

// Config selects the project and registry a watcher keeps current.
type Config struct {
	Root        string
	Registry    *registry.Registry
	IdleTimeout time.Duration
	Debounce    time.Duration
	OnError     func(error)
}

// Watcher follows project skill roots discovered by the last scan. A new root
// elsewhere is picked up after skillex refresh changes the registry and the
// next request reloads the watch set.
type Watcher struct {
	root     string
	reg      *registry.Registry
	idle     time.Duration
	debounce time.Duration
	onError  func(error)

	mu       sync.Mutex
	rearmMu  sync.Mutex
	jobs     sync.WaitGroup
	armed    bool
	writing  int
	fs       *fsnotify.Watcher
	roots    []string
	watched  map[string]struct{}
	pending  map[string]*time.Timer
	stamp    fileStamp
	lastBusy time.Time
	cancel   context.CancelFunc
}

// New starts the idle clock. Call Arm to scan and watch, and Close to stop.
func New(cfg Config) *Watcher {
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = DefaultIdleTimeout
	}
	if cfg.Debounce <= 0 {
		cfg.Debounce = DefaultDebounce
	}
	if abs, err := filepath.Abs(cfg.Root); err == nil {
		cfg.Root = abs
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &Watcher{
		root:     cfg.Root,
		reg:      cfg.Registry,
		idle:     cfg.IdleTimeout,
		debounce: cfg.Debounce,
		onError:  cfg.OnError,
		watched:  map[string]struct{}{},
		pending:  map[string]*time.Timer{},
		lastBusy: time.Now(),
		cancel:   cancel,
	}
	go w.idleLoop(ctx)
	return w
}

// Arm scans project skill roots, indexes those skills, and watches the roots.
func (w *Watcher) Arm() error {
	return w.rearm()
}

// Close stops the idle loop and the file watches.
func (w *Watcher) Close() {
	w.cancel()
	w.disarm()
	w.jobs.Wait()
}

// Armed reports whether file watches are currently registered.
func (w *Watcher) Armed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.armed
}

// BeforeRequest rearms watches after an idle timeout or after skillex refresh
// replaces the registry. Call it at the start of each MCP request.
func (w *Watcher) BeforeRequest() {
	w.touch()
	if !w.shouldRearm() {
		return
	}
	if err := w.rearm(); err != nil {
		w.report(err)
	}
}

func (w *Watcher) rearm() error {
	w.rearmMu.Lock()
	defer w.rearmMu.Unlock()

	w.disarm()
	skills, roots, errs := scanner.DiscoverAgentSkills(w.root)
	for _, err := range errs {
		w.report(err)
	}
	if err := w.syncDiscovered(skills, len(errs) == 0); err != nil {
		return err
	}

	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.roots = roots
	w.mu.Unlock()
	for _, root := range roots {
		w.watchTree(fs, root, false)
	}
	w.mu.Lock()
	w.fs = fs
	w.armed = true
	w.lastBusy = time.Now()
	w.noteStampLocked()
	w.mu.Unlock()
	go w.loop(fs)
	return nil
}

func (w *Watcher) disarm() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.disarmLocked()
}

func (w *Watcher) disarmLocked() {
	if w.fs != nil {
		_ = w.fs.Close()
		w.fs = nil
	}
	for _, timer := range w.pending {
		if timer.Stop() {
			w.jobs.Done()
		}
	}
	w.pending = map[string]*time.Timer{}
	w.watched = map[string]struct{}{}
	w.armed = false
}

func (w *Watcher) loop(fs *fsnotify.Watcher) {
	for {
		select {
		case event, ok := <-fs.Events:
			if !ok {
				return
			}
			w.handle(fs, event)
		case err, ok := <-fs.Errors:
			if !ok {
				return
			}
			w.report(err)
		}
	}
}

func (w *Watcher) handle(fs *fsnotify.Watcher, event fsnotify.Event) {
	if event.Op&^fsnotify.Chmod == 0 {
		return
	}
	w.touch()
	if !w.underRoot(event.Name) {
		return
	}
	info, err := os.Lstat(event.Name)
	if err == nil && info.IsDir() && event.Has(fsnotify.Create) {
		dir := event.Name
		w.watchTree(fs, dir, true)
		w.scheduleKey("tree:"+dir, func() {
			w.mu.Lock()
			current := w.fs
			w.mu.Unlock()
			if current != fs {
				return
			}
			w.watchTree(fs, dir, true)
		})
		return
	}
	if rel := skillMarkdownRel(w.root, event.Name); rel != "" {
		w.schedule(rel)
		return
	}
	if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
		w.scheduleForget(event.Name)
	}
}

func (w *Watcher) watchTree(fs *fsnotify.Watcher, dir string, schedule bool) {
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d == nil {
			return nil
		}
		if d.IsDir() {
			if skipWatchDir(d.Name()) && path != dir {
				return filepath.SkipDir
			}
			if path != dir && !w.underRoot(path) {
				return filepath.SkipDir
			}
			w.addWatch(fs, path)
			return nil
		}
		if schedule && d.Name() == "SKILL.md" {
			if rel := skillMarkdownRel(w.root, path); rel != "" {
				w.schedule(rel)
			}
		}
		return nil
	})
	if err != nil {
		w.report(err)
	}
}

func (w *Watcher) addWatch(fs *fsnotify.Watcher, path string) {
	w.mu.Lock()
	if w.fs != nil && w.fs != fs {
		w.mu.Unlock()
		return
	}
	if _, ok := w.watched[path]; ok {
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()
	if err := fs.Add(path); err != nil {
		if !errors.Is(err, fsnotify.ErrClosed) {
			w.report(err)
		}
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fs != nil && w.fs != fs {
		return
	}
	w.watched[path] = struct{}{}
}

func (w *Watcher) schedule(rel string) {
	w.scheduleKey(rel, func() { w.syncOne(rel) })
}

func (w *Watcher) scheduleForget(dir string) {
	w.scheduleKey("forget:"+dir, func() { w.forgetMissing(dir) })
}

func (w *Watcher) scheduleKey(key string, fn func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if timer := w.pending[key]; timer != nil {
		timer.Reset(w.debounce)
		return
	}
	w.jobs.Add(1)
	w.pending[key] = time.AfterFunc(w.debounce, func() {
		defer w.jobs.Done()
		w.mu.Lock()
		delete(w.pending, key)
		w.mu.Unlock()
		w.touch()
		fn()
	})
}

func (w *Watcher) syncOne(rel string) {
	w.beginWrite()
	defer w.endWrite()

	abs := filepath.Join(w.root, filepath.FromSlash(rel))
	if _, err := os.Stat(abs); err != nil {
		if os.IsNotExist(err) {
			if delErr := w.reg.DeleteSkillByPath(rel); delErr != nil {
				w.report(delErr)
			}
			return
		}
		w.report(err)
		return
	}
	files, err := scanner.ReadAgentSkill(w.root, abs)
	if err != nil {
		w.report(err)
		return
	}
	if err := applySkillFiles(w.reg, files); err != nil {
		w.report(err)
	}
}

func (w *Watcher) forgetMissing(dir string) {
	w.beginWrite()
	defer w.endWrite()

	relDir, err := filepath.Rel(w.root, dir)
	if err != nil {
		w.report(err)
		return
	}
	relDir = filepath.ToSlash(relDir)
	paths, err := w.reg.PathsBySource("agent")
	if err != nil {
		w.report(err)
		return
	}
	for _, skillPath := range paths {
		if skillPath != relDir && !strings.HasPrefix(skillPath, relDir+"/") {
			continue
		}
		abs := filepath.Join(w.root, filepath.FromSlash(skillPath))
		if _, statErr := os.Stat(abs); os.IsNotExist(statErr) {
			if delErr := w.reg.DeleteSkillByPath(skillPath); delErr != nil {
				w.report(delErr)
			}
		}
	}
}

func (w *Watcher) syncDiscovered(skills []scanner.SkillFile, allowDelete bool) error {
	w.beginWrite()
	defer w.endWrite()

	grouped := groupSkillFiles(skills)
	seen := map[string]bool{}
	for rel, files := range grouped {
		seen[rel] = true
		if err := applySkillFiles(w.reg, files); err != nil {
			return err
		}
	}
	if !allowDelete {
		return nil
	}
	paths, err := w.reg.PathsBySource("agent")
	if err != nil {
		return err
	}
	for _, skillPath := range paths {
		if !seen[skillPath] {
			if err := w.reg.DeleteSkillByPath(skillPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *Watcher) beginWrite() {
	w.mu.Lock()
	w.writing++
	w.mu.Unlock()
}

func (w *Watcher) endWrite() {
	w.mu.Lock()
	w.noteStampLocked()
	w.writing--
	w.mu.Unlock()
}

func (w *Watcher) shouldRearm() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.writing > 0 {
		return false
	}
	if !w.armed {
		return true
	}
	return stampDB(w.reg.Path()) != w.stamp
}

func (w *Watcher) underRoot(path string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	clean := filepath.Clean(path)
	for _, root := range w.roots {
		if clean == root || strings.HasPrefix(clean, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (w *Watcher) touch() {
	w.mu.Lock()
	w.lastBusy = time.Now()
	w.mu.Unlock()
}

func (w *Watcher) idleLoop(ctx context.Context) {
	interval := w.idle / 4
	if interval < 20*time.Millisecond {
		interval = 20 * time.Millisecond
	}
	if interval > 30*time.Second {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.mu.Lock()
			quiet := w.armed && time.Since(w.lastBusy) >= w.idle
			w.mu.Unlock()
			if quiet {
				w.disarm()
			}
		}
	}
}

func (w *Watcher) noteStampLocked() {
	w.stamp = stampDB(w.reg.Path())
}

func (w *Watcher) report(err error) {
	if err != nil && w.onError != nil {
		w.onError(err)
	}
}

type fileStamp struct {
	mod  [3]int64
	size [3]int64
	ok   [3]bool
}

func stampDB(path string) fileStamp {
	var stamp fileStamp
	for i, suffix := range []string{"", "-wal", "-shm"} {
		info, err := os.Stat(path + suffix)
		if err != nil {
			continue
		}
		stamp.ok[i] = true
		stamp.mod[i] = info.ModTime().UnixNano()
		stamp.size[i] = info.Size()
	}
	return stamp
}

func skillMarkdownRel(root, eventPath string) string {
	rel, err := filepath.Rel(root, eventPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	rel = filepath.ToSlash(rel)
	switch filepath.Base(eventPath) {
	case "SKILL.md":
		return rel
	case "SKILL.test.md":
		return strings.TrimSuffix(rel, ".test.md") + ".md"
	default:
		return ""
	}
}

func skipWatchDir(name string) bool {
	switch name {
	case ".git", ".skillex", "node_modules":
		return true
	default:
		return false
	}
}

func groupSkillFiles(files []scanner.SkillFile) map[string][]scanner.SkillFile {
	grouped := map[string][]scanner.SkillFile{}
	for _, file := range files {
		rel := file.RelPath
		if file.IsTest {
			rel = file.TestFor
		}
		grouped[rel] = append(grouped[rel], file)
	}
	return grouped
}

func applySkillFiles(reg *registry.Registry, files []scanner.SkillFile) error {
	var skill *scanner.SkillFile
	var tests []scanner.SkillFile
	for i := range files {
		if files[i].IsTest {
			tests = append(tests, files[i])
			continue
		}
		if skill == nil {
			skill = &files[i]
		}
	}
	if skill == nil {
		return nil
	}
	var scenarios []registry.TestScenario
	for _, testFile := range tests {
		parsed, _, err := validator.ParseTestFile(testFile.AbsPath)
		if err != nil {
			return err
		}
		if parsed == nil {
			continue
		}
		for _, scenario := range parsed.Scenarios {
			scenarios = append(scenarios, registry.TestScenario{
				Name:        scenario.Name,
				Prompt:      scenario.Prompt,
				ExtraSkills: scenario.ExtraSkills,
				Criteria:    scenario.Criteria,
			})
		}
	}
	id, err := reg.InsertSkill(registry.Skill{
		Path:        skill.RelPath,
		Content:     skill.Body,
		Name:        skill.Frontmatter.Name,
		Description: skill.Frontmatter.Description,
		Visibility:  skill.Visibility,
		SourceType:  skill.SourceType,
		Topics:      skill.Frontmatter.Topics,
		Tags:        skill.Frontmatter.Tags,
		Scopes:      skill.ExplicitScopes,
	})
	if err != nil {
		return err
	}
	if err := reg.DeleteTestsForSkill(id); err != nil {
		return err
	}
	for _, scenario := range scenarios {
		scenario.SkillID = id
		if err := reg.InsertTestScenario(scenario); err != nil {
			return err
		}
	}
	return nil
}
