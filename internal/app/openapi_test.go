package app_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/dombyte/pvmon/docs"
	"github.com/dombyte/pvmon/internal/http/router"
)

// TestOpenAPIContract keeps docs/openapi.yaml and the router in step (spec 15). The
// document must contain each route and no other operation. Each reference must resolve.
func TestOpenAPIContract(t *testing.T) {
	t.Parallel()
	var doc map[string]any
	require.NoError(t, yaml.Unmarshal(docs.Spec(), &doc))
	paths, ok := doc["paths"].(map[string]any)
	require.True(t, ok)
	var documented []string
	for path, item := range paths {
		ops, ok := item.(map[string]any)
		require.True(t, ok, path)
		for method := range ops {
			if method != "parameters" {
				documented = append(documented, strings.ToUpper(method)+" "+path)
			}
		}
	}
	// /ws is not describable in OpenAPI; /docs/ and / serve files.
	routes := slices.DeleteFunc(router.Patterns(), func(p string) bool {
		return p == "GET /ws" || p == "GET /docs/" || p == "GET /"
	})
	assert.ElementsMatch(t, routes, documented)
	checkRefs(t, doc, doc)
}

func checkRefs(t *testing.T, root map[string]any, node any) {
	t.Helper()
	switch n := node.(type) {
	case map[string]any:
		if ref, ok := n["$ref"].(string); ok {
			var cur any = root
			for _, key := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
				m, _ := cur.(map[string]any)
				cur = m[key]
			}
			assert.NotNil(t, cur, "unresolved %s", ref)
		}
		for _, v := range n {
			checkRefs(t, root, v)
		}
	case []any:
		for _, v := range n {
			checkRefs(t, root, v)
		}
	}
}
