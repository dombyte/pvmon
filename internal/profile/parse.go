package profile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ProblemsError lists every problem of a profile, each prefixed with its path.
type ProblemsError struct {
	Profile  string
	Problems []string
}

func (e *ProblemsError) Error() string {
	return fmt.Sprintf("profile %s: %s", e.Profile, strings.Join(e.Problems, "; "))
}

// Parse decodes a profile strictly (unknown keys are errors) and validates it.
func Parse(data []byte) (*Profile, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var p Profile
	if err := dec.Decode(&p); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("profile: empty document")
		}
		return nil, fmt.Errorf("profile: decode: %w", err)
	}
	if probs := Validate(&p); len(probs) > 0 {
		sort.Strings(probs)
		return nil, &ProblemsError{Profile: p.ID, Problems: probs}
	}
	return &p, nil
}
