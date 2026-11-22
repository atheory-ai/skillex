package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	sigverify "github.com/sigstore/sigstore-go/pkg/verify"
)

const PackRegistry = "atheory-ai/skillex-packs"
const producerRepo = "https://github.com/" + PackRegistry
const releaseWorkflow = ".github/workflows/release-pack.yml"

// PackRelease authenticates two independent claims about the same archive:
// its blob signature and its SLSA build statement from the tagged workflow.
func PackRelease(name, version string, archive, signature, attestations []byte) error {
	trusted, err := root.NewTrustedRootFromJSON(trustedRootJSON)
	if err != nil {
		return err
	}
	ref := "refs/tags/pack/" + name + "/v" + version
	identity := producerRepo + "/" + releaseWorkflow + "@" + ref
	digest := sha256.Sum256(archive)
	signed, err := parseBundle(archive, signature)
	if err != nil {
		return fmt.Errorf("reading archive signature: %w", err)
	}
	result, err := verifyDigest(signed, trusted, identity, digest)
	if err != nil {
		return fmt.Errorf("archive signature verification failed: %w", err)
	}
	if result.Statement != nil {
		return errors.New("expected an archive blob signature")
	}
	var page struct {
		Attestations []struct{ Bundle json.RawMessage }
	}
	if err := json.Unmarshal(attestations, &page); err != nil {
		return fmt.Errorf("reading archive attestations: %w", err)
	}
	if len(page.Attestations) > 100 {
		return errors.New("archive attestations exceed GitHub's maximum page size")
	}
	var lastErr error
	for _, attestation := range page.Attestations {
		var b bundle.Bundle
		if err := b.UnmarshalJSON(attestation.Bundle); err != nil {
			lastErr = err
			continue
		}
		verified, err := verifyDigest(&b, trusted, identity, digest)
		if err != nil {
			lastErr = err
			continue
		}
		if err := checkBuildStatement(verified, name+"-"+version+".tar.gz", ref, digest); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return errors.Join(errors.New("no verified build provenance for "+name+"@"+version), lastErr)
}

func checkBuildStatement(verified *sigverify.VerificationResult, filename, ref string, digest [sha256.Size]byte) error {
	stmt := verified.Statement
	if stmt.GetType() != "https://in-toto.io/Statement/v1" || stmt.GetPredicateType() != "https://slsa.dev/provenance/v1" {
		return errors.New("expected a SLSA v1 build statement")
	}
	predicate := stmt.GetPredicate().GetFields()
	definition := predicate["buildDefinition"].GetStructValue().GetFields()
	workflow := definition["externalParameters"].GetStructValue().GetFields()["workflow"].GetStructValue().GetFields()
	builder := predicate["runDetails"].GetStructValue().GetFields()["builder"].GetStructValue().GetFields()
	if definition["buildType"].GetStringValue() != "https://actions.github.io/buildtypes/workflow/v1" ||
		workflow["repository"].GetStringValue() != producerRepo || workflow["path"].GetStringValue() != releaseWorkflow ||
		workflow["ref"].GetStringValue() != ref || builder["id"].GetStringValue() != producerRepo+"/"+releaseWorkflow+"@"+ref {
		return errors.New("build provenance identifies a different workflow or release")
	}
	for _, subject := range stmt.GetSubject() {
		if subject.GetName() == filename && subject.GetDigest()["sha256"] == hex.EncodeToString(digest[:]) {
			return nil
		}
	}
	return errors.New("build provenance lacks the expected archive subject")
}
