// Package schema embeds the frozen v1 JSON Schemas for synca command output.
package schema

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// Version is the JSON contract version. It is independent of core.Version.
const Version = 1

//go:embed v1/*.json
var files embed.FS

// Draft is the JSON Schema dialect these documents use.
const Draft = "https://json-schema.org/draft/2020-12/schema"

// Read returns one embedded schema document, for example "version.json".
func Read(name string) ([]byte, error) {
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return nil, fmt.Errorf("invalid schema name %q", name)
	}
	b, err := fs.ReadFile(files, "v1/"+name)
	if err != nil {
		return nil, fmt.Errorf("schema %s: %w", name, err)
	}
	if !json.Valid(b) {
		return nil, fmt.Errorf("schema %s is not valid JSON", name)
	}
	return b, nil
}

// Names lists the embedded schema files.
func Names() ([]string, error) {
	ents, err := fs.ReadDir(files, "v1")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(ents))
	for _, ent := range ents {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		names = append(names, ent.Name())
	}
	sort.Strings(names)
	return names, nil
}
