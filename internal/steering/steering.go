package steering

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"strings"
)

// Ref is the stable Skillex read reference for the current core guidance.
const Ref = "core:steering"

const Source = "skillex"
const SkillPrefix = Source + ":"

//go:embed skills/verify-behavior.md
var verifyBehavior string

//go:embed skills/recheck-assumptions.md
var recheckAssumptions string

//go:embed skills/minimal-comments.md
var minimalComments string

// Skill is a bundled Markdown skill and the cue used when its preset is enabled.
type Skill struct {
	ID, Name, When, Topic, Content string
}

func (s Skill) Ref() string { return SkillPrefix + s.ID }

// Skills returns the catalog in stable order.
func Skills() []Skill {
	return []Skill{
		{"minimal-comments", "Minimal comments", "Avoid comments that restate code. Preserve required documentation and explanations of constraints.", "code-style", minimalComments},
		{"verify-behavior", "Verify behavior", "Before declaring a change done, verify its behavior through the normal entry point.", "verification", verifyBehavior},
		{"recheck-assumptions", "Recheck assumptions", "If two fixes based on the same assumption fail the same check, test the assumption before trying again.", "debugging", recheckAssumptions},
	}
}

// FindSkill resolves a name in the reserved Skillex skill namespace.
func FindSkill(ref string) (Skill, bool) {
	for _, skill := range Skills() {
		if skill.Ref() == ref {
			return skill, true
		}
	}
	return Skill{}, false
}

type Pointer struct {
	Name string `json:"name"`
	When string `json:"when"`
	Ref  string `json:"ref"`
}

// Marker lets a conversation recognize the guidance without receiving its text.
type Marker struct {
	Ref      string `json:"ref"`
	Revision string `json:"revision"`
}

// Guidance is the resolved text for the selected built-in presets.
type Guidance struct {
	Text     string
	Pointers []Pointer
	revision string
}

func (g *Guidance) Marker() *Marker {
	if g == nil {
		return nil
	}
	return &Marker{Ref: Ref, Revision: g.revision}
}

// Resolve validates preset names and composes their text in configuration order.
func Resolve(presets []string) (*Guidance, error) {
	if len(presets) == 0 {
		return nil, nil
	}
	pointers := make([]Pointer, 0, len(presets))
	var body strings.Builder
	body.WriteString("When a cue applies, read its skill if it is missing from context.\n\n")
	var revisionInput []byte
	seen := make(map[string]bool, len(presets))
	for _, preset := range presets {
		if seen[preset] {
			return nil, fmt.Errorf("duplicate core preset %q", preset)
		}
		seen[preset] = true
		skill, found := FindSkill(SkillPrefix + preset)
		if !found {
			return nil, fmt.Errorf("unknown core preset %q", preset)
		}
		pointers = append(pointers, Pointer{Name: skill.ID, When: skill.When, Ref: skill.Ref()})
		fmt.Fprintf(&body, "- %s (`%s`)\n", skill.When, skill.Ref())
		contentDigest := sha256.Sum256([]byte(skill.Content))
		revisionInput = append(revisionInput, contentDigest[:]...)
	}
	text := body.String()
	digest := sha256.Sum256(append(revisionInput, text...))
	return &Guidance{Text: text, Pointers: pointers, revision: hex.EncodeToString(digest[:8])}, nil
}
