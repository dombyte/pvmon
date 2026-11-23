package profiles

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFS(t *testing.T) {
	t.Parallel()
	files, err := fs.Glob(FS(), "*/*.yaml")
	require.NoError(t, err)
	assert.Contains(t, files, "solis/s6-hybrid.yaml")
}
