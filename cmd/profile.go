package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/dombyte/pvmon/internal/profile"
)

// errInvalidProfile marks a validation failure already printed per file.
var errInvalidProfile = errors.New("invalid profile")

// runProfile implements "pvmon profile validate <file>…" (spec 4.5).
func runProfile(e env) error {
	if len(e.args) < 2 || e.args[0] != "validate" {
		return fmt.Errorf("profile validate <file>…: %w", errUsage)
	}
	failed := false
	for _, path := range e.args[1:] {
		if !validateProfileFile(e, path) {
			failed = true
		}
	}
	if failed {
		return errInvalidProfile
	}
	return nil
}

func validateProfileFile(e env, path string) bool {
	data, err := os.ReadFile(path)
	if err == nil {
		var p *profile.Profile
		p, err = profile.Parse(data)
		if err == nil {
			writeLine(e.stdout, fmt.Sprintf("%s: ok (%s v%d)", path, p.ID, p.Version))
			return true
		}
	}
	var pe *profile.ProblemsError
	if errors.As(err, &pe) {
		for _, prob := range pe.Problems {
			writeLine(e.stdout, fmt.Sprintf("%s: %s", path, prob))
		}
		return false
	}
	writeLine(e.stdout, fmt.Sprintf("%s: %v", path, err))
	return false
}
