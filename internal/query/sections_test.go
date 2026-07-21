package query

import "testing"

func TestSectionsAndSelectSection(t *testing.T) {
	content := "# Parent\nintro\n\n## Child\nbody\n\n```md\n# not-a-heading\n```\n\n## Child\nsecond\n\n# Next\nend"
	sections := Sections(content)
	if len(sections) != 4 || sections[2].ID != "child-1" {
		t.Fatalf("unexpected sections: %#v", sections)
	}
	selected, ok := SelectSection(content, "parent")
	if !ok || selected == content || !contains(selected, "second") || contains(selected, "# Next") {
		t.Fatalf("unexpected selected section: %q", selected)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
