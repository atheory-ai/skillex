package registry

import "testing"

// A skill listed under several scope rules is inserted more than once with the same path. The
// repeat insert hits `ON CONFLICT(path) DO UPDATE`, where res.LastInsertId() is unreliable: it
// returns the rowid of the most recent real INSERT on the connection — a *different* skill — with
// no error. The old code only fell back to a path lookup on a non-nil error, so it returned the
// wrong id and re-attached the skill's child topic/tag/scope rows to another skill (and, across
// pooled connections where LastInsertId is 0, failed outright with "FOREIGN KEY constraint
// failed"). InsertSkill must resolve the id by its unique path instead.
func TestInsertSkill_DuplicatePath_ResolvesCorrectID(t *testing.T) {
	reg := newTestRegistry(t)

	mk := func(path string, topics []string) Skill {
		return Skill{
			Path:       path,
			Content:    "# " + path,
			Visibility: "public",
			SourceType: "local",
			Topics:     topics,
			Scopes:     []string{"components/**"},
		}
	}

	idA, err := reg.InsertSkill(mk("skills/a.md", []string{"a-topic"}))
	if err != nil {
		t.Fatalf("insert A: %v", err)
	}
	idB, err := reg.InsertSkill(mk("skills/b.md", []string{"b-topic"}))
	if err != nil {
		t.Fatalf("insert B: %v", err)
	}
	if idA == idB {
		t.Fatalf("distinct skills must have distinct ids, both = %d", idA)
	}

	// Re-insert A (as a duplicate scope reference would). The most recent real INSERT was B, so a
	// LastInsertId-based id would wrongly come back as idB.
	idA2, err := reg.InsertSkill(mk("skills/a.md", []string{"a-topic-updated"}))
	if err != nil {
		t.Fatalf("re-insert A (upsert) must not fail: %v", err)
	}
	if idA2 != idA {
		t.Fatalf("upsert of A returned id %d, want A's id %d (stale LastInsertId, likely B=%d)", idA2, idA, idB)
	}

	// A's updated topic must be attached to A, and B's topic left untouched.
	assertTopic := func(skillID int64, want string) {
		var got string
		var n int
		if err := reg.db.QueryRow(`SELECT count(*) FROM skill_topics WHERE skill_id = ?`, skillID).Scan(&n); err != nil {
			t.Fatalf("count topics for %d: %v", skillID, err)
		}
		if n != 1 {
			t.Fatalf("expected 1 topic for skill_id=%d, got %d", skillID, n)
		}
		if err := reg.db.QueryRow(`SELECT topic FROM skill_topics WHERE skill_id = ?`, skillID).Scan(&got); err != nil {
			t.Fatalf("read topic for %d: %v", skillID, err)
		}
		if got != want {
			t.Fatalf("skill_id=%d topic = %q, want %q", skillID, got, want)
		}
	}
	assertTopic(idA, "a-topic-updated")
	assertTopic(idB, "b-topic")
}
