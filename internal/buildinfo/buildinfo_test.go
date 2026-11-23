package buildinfo_test

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dombyte/pvmon/internal/buildinfo"
)

func TestNew(t *testing.T) {
	t.Parallel()

	info := buildinfo.New("", "abc", "2026-01-01")
	assert.Equal(t, "dev", info.Version)
	assert.Equal(t, "abc", info.Commit)
	assert.Equal(t, "2026-01-01", info.BuildDate)
	assert.Equal(t, runtime.Version(), info.GoVersion)
	assert.Equal(t, "1.2.3-abc", buildinfo.New("1.2.3-abc", "", "").Version)
}

func TestString(t *testing.T) {
	t.Parallel()

	got := buildinfo.New("1.0.0", "abc", "today").String()
	assert.Equal(t, "pvmon 1.0.0 (commit abc, built today, "+runtime.Version()+")", got)
	assert.Contains(t, buildinfo.New("", "", "").String(), "commit unknown, built unknown")
}
