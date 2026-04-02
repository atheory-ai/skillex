package frontmatter

import (
	"bytes"
	"strings"

	"gopkg.in/yaml.v3"
)

// Frontmatter holds the YAML frontmatter metadata from a skill file.
type Frontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Topics      []string `yaml:"topics"`
	Tags        []string `yaml:"tags"`
	Source      string   `yaml:"source"`
	Reviewed    string   `yaml:"reviewed"`
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
