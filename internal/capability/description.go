package capability

import (
	"encoding/json"
	"errors"
	"fmt"
)

const (
	DefaultDescriptionMaxBytes = 24 * 1024
	MaximumDescriptionMaxBytes = 64 * 1024
)

var (
	ErrDescriptionBudgetInvalid = errors.New("capability description byte budget is invalid")
	ErrDescriptionTooLarge      = errors.New("capability description exceeds byte budget")
)

func MarshalDescription(selected Capability, maxBytes int) ([]byte, error) {
	if maxBytes == 0 {
		maxBytes = DefaultDescriptionMaxBytes
	}
	if maxBytes < 1 || maxBytes > MaximumDescriptionMaxBytes {
		return nil, fmt.Errorf("%w: max_bytes must be between 1 and %d", ErrDescriptionBudgetInvalid, MaximumDescriptionMaxBytes)
	}
	encoded, err := json.MarshalIndent(selected, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxBytes {
		return nil, fmt.Errorf("%w: encoded description is %d bytes, budget is %d", ErrDescriptionTooLarge, len(encoded), maxBytes)
	}
	return encoded, nil
}
