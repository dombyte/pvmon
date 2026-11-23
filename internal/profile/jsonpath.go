package profile

import (
	"fmt"
	"strconv"
	"strings"
)

// Lookup evaluates a JSONPath of the supported subset ($.a.b[0].c) on decoded JSON.
func Lookup(doc any, path string) (any, bool) {
	if !ValidPath(path) {
		return nil, false
	}
	cur := doc
	for _, tok := range pathTokens(path) {
		var ok bool
		cur, ok = step(cur, tok)
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// pathTokens splits "$.a.b[0]" into "a", "b", "[0]".
func pathTokens(path string) []string {
	s := strings.TrimPrefix(path, "$")
	s = strings.ReplaceAll(s, "[", ".[")
	var out []string
	for _, t := range strings.Split(s, ".") {
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func step(cur any, tok string) (any, bool) {
	if strings.HasPrefix(tok, "[") {
		arr, ok := cur.([]any)
		if !ok {
			return nil, false
		}
		i, err := strconv.Atoi(strings.Trim(tok, "[]"))
		if err != nil || i >= len(arr) {
			return nil, false
		}
		return arr[i], true
	}
	obj, ok := cur.(map[string]any)
	if !ok {
		return nil, false
	}
	v, ok := obj[tok]
	return v, ok
}

// number converts a JSON value to float64: numbers, bools (1/0) and numeric strings.
func number(v any) (float64, error) {
	switch x := v.(type) {
	case float64:
		return x, nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case string:
		f, err := strconv.ParseFloat(x, 64)
		if err != nil {
			return 0, fmt.Errorf("not a number: %q", x)
		}
		return f, nil
	}
	return 0, fmt.Errorf("not a number: %T", v)
}
