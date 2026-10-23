package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// The shared-file editor changes only the top-level "hooks" member of a JSON
// settings file. Every other byte of the file is kept as it was (key order,
// spacing, the odd `"key" : value` style of Devin's config). Inside "hooks"
// the structure and order are kept and whitespace is normalized.

type jsonMember struct {
	key        string
	start, end int // byte span of the value in the original data
}

// topLevelMembers returns the members of a JSON object with value spans.
func topLevelMembers(data []byte) ([]jsonMember, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("top level is not a JSON object")
	}
	var out []jsonMember
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := kt.(string)
		if !ok {
			return nil, errors.New("object key is not a string")
		}
		i := int(dec.InputOffset())
		for i < len(data) && (data[i] == ' ' || data[i] == '\t' || data[i] == '\n' || data[i] == '\r' || data[i] == ':') {
			i++
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		out = append(out, jsonMember{key: key, start: i, end: int(dec.InputOffset())})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if rest := bytes.TrimSpace(data[dec.InputOffset():]); len(rest) > 0 {
		return nil, errors.New("trailing data after the JSON object")
	}
	return out, nil
}

// hooksEvent is one event's array of raw matcher groups.
type hooksEvent struct {
	name  string
	items [][]byte
}

// hooksDoc is the parsed "hooks" object, event order preserved.
type hooksDoc struct{ events []*hooksEvent }

func (d *hooksDoc) find(name string) *hooksEvent {
	for _, e := range d.events {
		if e.name == name {
			return e
		}
	}
	return nil
}

func (d *hooksDoc) drop(name string) {
	for i, e := range d.events {
		if e.name == name {
			d.events = append(d.events[:i], d.events[i+1:]...)
			return
		}
	}
}

func parseHooksValue(raw []byte) (*hooksDoc, error) {
	members, err := topLevelMembers(raw)
	if err != nil {
		return nil, fmt.Errorf("\"hooks\" is not an object: %v", err)
	}
	doc := &hooksDoc{}
	for _, m := range members {
		var arr []json.RawMessage
		if err := json.Unmarshal(raw[m.start:m.end], &arr); err != nil {
			return nil, fmt.Errorf("hooks.%s is not an array", m.key)
		}
		ev := &hooksEvent{name: m.key}
		for _, it := range arr {
			ev.items = append(ev.items, append([]byte(nil), it...))
		}
		doc.events = append(doc.events, ev)
	}
	return doc, nil
}

// sharedHooks is the result of reading a settings file.
type sharedHooks struct {
	doc       *hooksDoc
	members   []jsonMember
	hooksIdx  int // index in members, -1 when the file has no "hooks" key
	data      []byte
	keyIndent string
	unit      string
}

func readSharedHooks(data []byte) (*sharedHooks, error) {
	s := &sharedHooks{doc: &hooksDoc{}, hooksIdx: -1, data: data, unit: detectIndentUnit(data)}
	if len(bytes.TrimSpace(data)) == 0 {
		s.keyIndent = s.unit
		return s, nil
	}
	members, err := topLevelMembers(data)
	if err != nil {
		return nil, err
	}
	s.members = members
	s.keyIndent = s.unit
	for i, m := range members {
		if m.key == "hooks" {
			s.hooksIdx = i
			doc, err := parseHooksValue(data[m.start:m.end])
			if err != nil {
				return nil, err
			}
			s.doc = doc
			s.keyIndent = lineIndent(data, m.start)
		}
	}
	return s, nil
}

func detectIndentUnit(data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed != line && strings.HasPrefix(trimmed, "\"") {
			return line[:len(line)-len(trimmed)]
		}
	}
	return "  "
}

func lineIndent(data []byte, pos int) string {
	start := bytes.LastIndexByte(data[:pos], '\n') + 1
	end := start
	for end < len(data) && (data[end] == ' ' || data[end] == '\t') {
		end++
	}
	return string(data[start:end])
}

func buildHooksValue(doc *hooksDoc, prefix, unit string) ([]byte, error) {
	var compact bytes.Buffer
	compact.WriteByte('{')
	for i, e := range doc.events {
		if i > 0 {
			compact.WriteByte(',')
		}
		k, err := encodeJSON(e.name)
		if err != nil {
			return nil, err
		}
		compact.Write(k)
		compact.WriteString(":[")
		for j, it := range e.items {
			if j > 0 {
				compact.WriteByte(',')
			}
			compact.Write(it)
		}
		compact.WriteByte(']')
	}
	compact.WriteByte('}')
	var out bytes.Buffer
	if err := json.Indent(&out, compact.Bytes(), prefix, unit); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// spliceHooks returns the file content with the "hooks" member replaced by doc.
func (s *sharedHooks) spliceHooks(doc *hooksDoc) ([]byte, error) {
	val, err := buildHooksValue(doc, s.keyIndent, s.unit)
	if err != nil {
		return nil, err
	}
	var out []byte
	switch {
	case s.hooksIdx >= 0:
		m := s.members[s.hooksIdx]
		out = append(append(append([]byte{}, s.data[:m.start]...), val...), s.data[m.end:]...)
	case len(s.members) > 0:
		last := s.members[len(s.members)-1]
		ins := append([]byte(",\n"+s.unit+"\"hooks\": "), val...)
		out = append(append(append([]byte{}, s.data[:last.end]...), ins...), s.data[last.end:]...)
	default:
		nl := ""
		if len(s.data) == 0 || bytes.HasSuffix(s.data, []byte("\n")) {
			nl = "\n"
		}
		body := val
		out = []byte("{\n" + s.unit + "\"hooks\": " + string(body) + "\n}" + nl)
	}
	if !json.Valid(out) {
		return nil, errors.New("edited settings are not valid JSON; nothing was written")
	}
	return out, nil
}
