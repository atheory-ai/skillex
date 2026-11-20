package packs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"slices"

	"github.com/atheory-ai/skillex/internal/packregistry"
)

type FileChange struct {
	Path   string  `json:"path"`
	Before *string `json:"before,omitempty"`
	After  *string `json:"after,omitempty"`
}

type activation struct {
	Skills     []SkillRef     `json:"skills"`
	Detectors  Detectors      `json:"detectors"`
	MCPServers []MCPServerRef `json:"mcp_servers"`
}

type UpdateReview struct {
	Registry            string             `json:"registry"`
	From                packregistry.Entry `json:"from"`
	To                  packregistry.Entry `json:"to"`
	Files               []FileChange       `json:"files"`
	Previous            *activation        `json:"previous_activation,omitempty"`
	PreviousSchemaError string             `json:"previous_schema_error,omitempty"`
	Proposed            activation         `json:"proposed_activation"`
}

type UpdatePreview struct {
	Updates  []UpdateReview `json:"updates"`
	Approval string         `json:"approval,omitempty"`
}

func PreviewUpdates(changes []packregistry.Update) (UpdatePreview, error) {
	preview := UpdatePreview{Updates: []UpdateReview{}}
	for _, change := range changes {
		next, err := Preview(change.To)
		if err != nil {
			return preview, err
		}
		old := change.From
		review := UpdateReview{Registry: old.Registry, From: old.Entry, To: next.Pack, Proposed: activation{next.Skills, next.Detectors, next.MCPServers}}
		previous, err := parseManifest(Filename, old.VerifiedFiles[Filename], old.VerifiedFiles)
		if err != nil {
			review.PreviousSchemaError = err.Error()
		} else {
			m := previous.Manifest
			review.Previous = &activation{m.Skills, m.Detectors, m.MCPServers}
		}
		paths := slices.AppendSeq(slices.Clone(next.Files), maps.Keys(old.VerifiedFiles))
		slices.Sort(paths)
		for _, path := range slices.Compact(paths) {
			a, had := old.VerifiedFiles[path]
			b, has := change.To.Files[path]
			if had == has && bytes.Equal(a, b) {
				continue
			}
			f := FileChange{Path: path}
			if had {
				v := string(a)
				f.Before = &v
			}
			if has {
				v := string(b)
				f.After = &v
			}
			review.Files = append(review.Files, f)
		}
		preview.Updates = append(preview.Updates, review)
	}
	if len(changes) > 0 {
		body, err := json.Marshal(preview.Updates)
		if err != nil {
			return preview, err
		}
		hash := sha256.Sum256(body)
		preview.Approval = hex.EncodeToString(hash[:])
	}
	return preview, nil
}
