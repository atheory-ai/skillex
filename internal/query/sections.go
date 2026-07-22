package query

import (
	"regexp"
	"strings"
)

var headingRE = regexp.MustCompile(`^(#{1,6})[ \t]+(.+?)[ \t]*#*[ \t]*$`)

// Sections extracts ATX headings outside fenced code blocks. IDs are stable
// within a skill and intentionally do not depend on database row ids.
func Sections(content string) []Section {
	var out []Section
	inFence := false
	seen := map[string]int{}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		m := headingRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		fullTitle := strings.TrimSpace(m[2])
		id := slug(fullTitle)
		seen[id]++
		if seen[id] > 1 {
			id += "-" + strconvItoa(seen[id]-1)
		}
		out = append(out, Section{ID: id, Title: compactText(fullTitle, 160), Level: len(m[1])})
		if len(out) == 8 {
			break
		}
	}
	return out
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// Tiny local integer formatting keeps this parser dependency-free.
func strconvItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// SelectSection returns a heading and its children, ending at the next heading
// with the same or shallower level.
func SelectSection(content, sectionID string) (string, bool) {
	lines := strings.Split(content, "\n")
	inFence := false
	seen := map[string]int{}
	start, level := -1, 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		m := headingRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		id := slug(strings.TrimSpace(m[2]))
		seen[id]++
		if seen[id] > 1 {
			id += "-" + strconvItoa(seen[id]-1)
		}
		currentLevel := len(m[1])
		if start >= 0 && currentLevel <= level {
			return strings.Join(lines[start:i], "\n"), true
		}
		if id == sectionID {
			start, level = i, currentLevel
		}
	}
	if start >= 0 {
		return strings.Join(lines[start:], "\n"), true
	}
	return "", false
}
