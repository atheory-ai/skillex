package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/packregistry"
	"github.com/atheory-ai/skillex/internal/steering"
)

// ReadTaskFiles snapshots local bindings before rule or dependency discovery.
// A nil entry represents an absent optional sibling test.
func ReadTaskFiles(project string, tasks []config.TaskBinding) (map[string][]byte, error) {
	if len(tasks) == 0 {
		return nil, nil
	}
	root, err := os.OpenRoot(project)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	files := make(map[string][]byte)
	for _, task := range tasks {
		if _, seen := files[task.Skill]; seen || strings.HasPrefix(task.Skill, steering.SkillPrefix) {
			continue
		}
		test := strings.TrimSuffix(task.Skill, ".md") + ".test.md"
		for _, path := range []string{task.Skill, test} {
			data, err := readTaskFile(root, path)
			if err != nil && (path != test || !os.IsNotExist(err)) {
				return nil, fmt.Errorf("task %q skill %q: %w", task.Name, path, err)
			}
			files[path] = data
		}
	}
	return files, nil
}

func readTaskFile(root *os.Root, path string) ([]byte, error) {
	name := filepath.FromSlash(path)
	info, err := root.Stat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("expected a regular Markdown file")
	}
	if dir, file, installed := packregistry.InstalledFilePath(filepath.Join(root.Name(), name)); installed {
		lock, err := packregistry.ValidateDirectory(dir)
		if err != nil {
			return nil, err
		}
		data, ok := lock.VerifiedFiles[file]
		if !ok {
			return nil, os.ErrNotExist
		}
		return data, nil
	}
	return root.ReadFile(name)
}
