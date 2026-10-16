package frontmatter

import (
	"bytes"
	"errors"
	"strings"

	"gopkg.in/yaml.v3"
)

// Frontmatter holds the YAML frontmatter metadata from a skill file.
type Frontmatter struct {
	Name        string     `yaml:"name"`
	Description string     `yaml:"description"`
	Topics      []string   `yaml:"topics"`
	Tags        []string   `yaml:"tags"`
	Source      string     `yaml:"source"`
	Reviewed    string     `yaml:"reviewed"`
	Paths       StringList `yaml:"paths"`
	Globs       StringList `yaml:"globs"`
}

// StringList unmarshals either a YAML sequence or a comma-separated string.
// Agent skill files use both shapes for the paths field.
type StringList []string

// UnmarshalYAML accepts a sequence or a scalar split on commas.
func (s *StringList) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.SequenceNode:
		var items []string
		if err := value.Decode(&items); err != nil {
			return err
		}
		*s = items
		return nil
	case yaml.ScalarNode:
		if strings.TrimSpace(value.Value) == "" {
			*s = nil
			return nil
		}
		parts := strings.Split(value.Value, ",")
		items := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				items = append(items, part)
			}
		}
		*s = items
		return nil
	default:
		return errors.New("expected a string or list")
	}
}

// PathGlobs returns the skill's path filters. paths wins over the legacy globs field.
func (f Frontmatter) PathGlobs() []string {
	if len(f.Paths) > 0 {
		return append([]string(nil), f.Paths...)
	}
	if len(f.Globs) > 0 {
		return append([]string(nil), f.Globs...)
	}
	return nil
}

// Parse separates YAML frontmatter from Markdown body.
// Returns the parsed frontmatter and the body content.
// If no frontmatter is found, returns an empty Frontmatter and the full content as body.
func Parse(content []byte) (Frontmatter, string, error) {
	s := string(content)
	if !strings.HasPrefix(s, "---") {
		return Frontmatter{}, s, nil
	}

	// Find the closing ---
	rest := s[3:]
	// Skip optional newline after opening ---
	if strings.HasPrefix(rest, "\n") {
		rest = rest[1:]
	} else if strings.HasPrefix(rest, "\r\n") {
		rest = rest[2:]
	}

	idx := strings.Index(rest, "\n---")
	if idx == -1 {
		// No closing delimiter found; treat whole thing as body
		return Frontmatter{}, s, nil
	}

	fmRaw := rest[:idx]
	body := rest[idx+4:] // skip \n---
	// Trim leading newline from body
	if strings.HasPrefix(body, "\n") {
		body = body[1:]
	} else if strings.HasPrefix(body, "\r\n") {
		body = body[2:]
	}

	var fm Frontmatter
	if err := yaml.NewDecoder(bytes.NewBufferString(fmRaw)).Decode(&fm); err != nil {
		return Frontmatter{}, s, err
	}

	return fm, body, nil
}

// FormatFrontmatter serializes frontmatter fields into a YAML block.
func FormatFrontmatter(fm Frontmatter) string {
	if len(fm.Topics) == 0 && len(fm.Tags) == 0 && fm.Source == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("---\n")
	if len(fm.Topics) > 0 {
		sb.WriteString("topics: [")
		sb.WriteString(strings.Join(fm.Topics, ", "))
		sb.WriteString("]\n")
	}
	if len(fm.Tags) > 0 {
		sb.WriteString("tags: [")
		sb.WriteString(strings.Join(fm.Tags, ", "))
		sb.WriteString("]\n")
	}
	if fm.Source != "" {
		sb.WriteString("source: ")
		sb.WriteString(fm.Source)
		sb.WriteString("\n")
	}
	if fm.Reviewed != "" {
		sb.WriteString("reviewed: ")
		sb.WriteString(fm.Reviewed)
		sb.WriteString("\n")
	}
	sb.WriteString("---\n")
	return sb.String()
}
