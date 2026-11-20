package packs

import (
	"fmt"
	"sort"

	"github.com/atheory-ai/skillex/internal/packregistry"
)

// Proposal describes the complete contents and activation of a verified archive.
type Proposal struct {
	Pack       packregistry.Entry `json:"pack"`
	Files      []string           `json:"files"`
	Contents   map[string]string  `json:"contents"`
	Skills     []SkillRef         `json:"skills"`
	Detectors  Detectors          `json:"detectors"`
	MCPServers []MCPServerRef     `json:"mcp_servers"`
	Installed  bool               `json:"installed"`
}

// Preview validates the prepared files in memory using the installed-pack schema.
func Preview(prepared *packregistry.Prepared) (*Proposal, error) {
	pack, err := parseManifest(Filename, prepared.Files[Filename], prepared.Files)
	if err != nil {
		return nil, err
	}
	entry := prepared.Lock.Entry
	if pack.Manifest.Name != entry.Name || pack.Manifest.Version != entry.Version {
		return nil, fmt.Errorf("pack identity differs from signed manifest")
	}
	proposal := &Proposal{Pack: entry, Skills: pack.Manifest.Skills, Detectors: pack.Manifest.Detectors, MCPServers: pack.Manifest.MCPServers, Contents: make(map[string]string, len(prepared.Files))}
	for name, data := range prepared.Files {
		proposal.Files = append(proposal.Files, name)
		proposal.Contents[name] = string(data)
	}
	sort.Strings(proposal.Files)
	return proposal, nil
}
