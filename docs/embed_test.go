package docs

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFS(t *testing.T) {
	t.Parallel()
	_, err := fs.Stat(FS(), ".gitkeep")
	assert.NoError(t, err)
	assert.Contains(t, string(Spec()), "openapi: 3.0.3")
}
