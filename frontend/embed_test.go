package frontend

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFS(t *testing.T) {
	t.Parallel()
	_, err := fs.Stat(FS(), ".gitkeep")
	assert.NoError(t, err)
}
