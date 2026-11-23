package app

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNamedLocation(t *testing.T) {
	t.Parallel()
	link := func(target string, err error) func(string) (string, error) {
		return func(string) (string, error) { return target, err }
	}
	noLink := link("", errors.New("no link"))
	tests := []struct {
		name, tz, want string
		readlink       func(string) (string, error)
	}{
		{"from TZ", "Europe/Berlin", "Europe/Berlin", noLink},
		{"TZ with colon", ":UTC", "UTC", noLink},
		{"from link", "", "Europe/Berlin", link("/usr/share/zoneinfo/Europe/Berlin", nil)},
		{"unknown zone", "Mars/Olympus", "Local", noLink},
		{"nothing", "", "Local", noLink},
		{"odd link", "", "Local", link("/etc/somewhere", nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, namedLocation(tt.tz, tt.readlink).String())
		})
	}
	assert.NotNil(t, LocalLocation())
}
