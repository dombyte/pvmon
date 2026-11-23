package pdf

import (
	_ "embed" // texts
	"encoding/json"
	"fmt"
)

// textsJSON holds every text of the PDF: key → [English, German].
//
//go:embed texts.json
var textsJSON []byte

// texts parses the texts per document, so the package keeps no global lookup map
// (AGENTS.md section 4).
func texts() (map[string][2]string, error) {
	var m map[string][2]string
	if err := json.Unmarshal(textsJSON, &m); err != nil {
		return nil, fmt.Errorf("pdf: texts: %w", err)
	}
	return m, nil
}
