package cli

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// validateSchema checks a JSON Schema draft 2020-12 document against an instance.
// It implements type, enum, required, properties, items, additionalProperties,
// and local $ref. Other keywords are ignored.
func validateSchema(schema, instance any) error {
	return validateNode(schema, schema, instance, "$", nil)
}

// validateNode checks instance against schema. root is the document $ref resolves in.
func validateNode(root, schema, instance any, path string, stack []string) error {
	sch, ok := schema.(map[string]any)
	if !ok {
		return fmt.Errorf("%s: schema is %T", path, schema)
	}
	if ref, ok := sch["$ref"].(string); ok {
		for _, seen := range stack {
			if seen == ref {
				return fmt.Errorf("%s: $ref cycle %s", path, ref)
			}
		}
		target, err := resolvePtr(root, ref)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		return validateNode(root, target, instance, path, append(stack, ref))
	}
	if raw, ok := sch["type"]; ok {
		if err := checkType(raw, instance, path); err != nil {
			return err
		}
	}
	if raw, ok := sch["enum"]; ok {
		enum, ok := raw.([]any)
		if !ok {
			return fmt.Errorf("%s: enum is not an array", path)
		}
		if !inEnum(enum, instance) {
			return fmt.Errorf("%s: %v is not in enum", path, instance)
		}
	}
	if raw, ok := sch["required"]; ok && instance != nil {
		req, ok := raw.([]any)
		if !ok {
			return fmt.Errorf("%s: required is not an array", path)
		}
		obj, ok := instance.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: required applies to an object", path)
		}
		for _, name := range req {
			key, ok := name.(string)
			if !ok {
				return fmt.Errorf("%s: required name is %T", path, name)
			}
			if _, exists := obj[key]; !exists {
				return fmt.Errorf("%s: missing required property %q", path, key)
			}
		}
	}
	if raw, ok := sch["properties"]; ok && isObject(instance) {
		props, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: properties is not an object", path)
		}
		obj := instance.(map[string]any)
		for key, value := range obj {
			prop, exists := props[key]
			if !exists {
				continue
			}
			if err := validateNode(root, prop, value, propPath(path, key), stack); err != nil {
				return err
			}
		}
	}
	if raw, ok := sch["additionalProperties"]; ok && isObject(instance) {
		if err := checkAdditional(root, sch, raw, instance.(map[string]any), path, stack); err != nil {
			return err
		}
	}
	if raw, ok := sch["items"]; ok && isArray(instance) {
		for i, item := range instance.([]any) {
			if err := validateNode(root, raw, item, fmt.Sprintf("%s[%d]", path, i), stack); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkAdditional(root, sch, raw any, obj map[string]any, path string, stack []string) error {
	props, _ := sch.(map[string]any)["properties"].(map[string]any)
	switch ap := raw.(type) {
	case bool:
		if ap {
			return nil
		}
		for key := range obj {
			if props != nil {
				if _, defined := props[key]; defined {
					continue
				}
			}
			return fmt.Errorf("%s: unexpected property %q", path, key)
		}
	case map[string]any:
		for key, value := range obj {
			if props != nil {
				if _, defined := props[key]; defined {
					continue
				}
			}
			if err := validateNode(root, ap, value, propPath(path, key), stack); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("%s: additionalProperties is %T", path, raw)
	}
	return nil
}

func checkType(raw, instance any, path string) error {
	types, err := typeNames(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	for _, typ := range types {
		if matchType(typ, instance) {
			return nil
		}
	}
	return fmt.Errorf("%s: expected type %s, got %s", path, strings.Join(types, "|"), jsonKind(instance))
}

func typeNames(raw any) ([]string, error) {
	switch v := raw.(type) {
	case string:
		return []string{v}, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			name, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("type entry is %T", item)
			}
			out = append(out, name)
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("type is empty")
		}
		return out, nil
	default:
		return nil, fmt.Errorf("type is %T", raw)
	}
}

func matchType(typ string, v any) bool {
	switch typ {
	case "object":
		return isObject(v)
	case "array":
		return isArray(v)
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "null":
		return v == nil
	case "number":
		_, ok := v.(float64)
		return ok
	case "integer":
		f, ok := v.(float64)
		return ok && !math.IsInf(f, 0) && !math.IsNaN(f) && f == math.Trunc(f)
	default:
		return false
	}
}

func inEnum(enum []any, v any) bool {
	for _, item := range enum {
		if item == nil || v == nil {
			if item == nil && v == nil {
				return true
			}
			continue
		}
		if item == v {
			return true
		}
	}
	return false
}

func resolvePtr(root any, ref string) (any, error) {
	if ref == "#" {
		return root, nil
	}
	if !strings.HasPrefix(ref, "#/") {
		return nil, fmt.Errorf("unsupported $ref %q", ref)
	}
	cur := root
	for _, seg := range strings.Split(ref[2:], "/") {
		seg = strings.ReplaceAll(seg, "~1", "/")
		seg = strings.ReplaceAll(seg, "~0", "~")
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("$ref %s is not an object at %q", ref, seg)
		}
		next, ok := obj[seg]
		if !ok {
			return nil, fmt.Errorf("$ref %s missing %q", ref, seg)
		}
		cur = next
	}
	return cur, nil
}

func isObject(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}

func isArray(v any) bool {
	_, ok := v.([]any)
	return ok
}

func propPath(path, key string) string {
	if strings.ContainsAny(key, ". \"[]") {
		return fmt.Sprintf("%s[%q]", path, key)
	}
	return path + "." + key
}

func TestValidatorKeywords(t *testing.T) {
	sch := map[string]any{
		"type":     "object",
		"required": []any{"n", "items"},
		"properties": map[string]any{
			"n":     map[string]any{"type": "integer", "enum": []any{float64(1), float64(2)}},
			"items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"tag":   map[string]any{"enum": []any{"a", "b"}},
		},
		"additionalProperties": false,
	}
	if err := validateSchema(sch, map[string]any{"n": float64(1), "items": []any{"x"}}); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema(sch, map[string]any{"n": float64(1), "items": []any{"x"}, "extra": true}); err == nil {
		t.Fatal("extra property accepted")
	}
	if err := validateSchema(sch, map[string]any{"n": float64(1)}); err == nil {
		t.Fatal("missing items accepted")
	}
	if err := validateSchema(sch, map[string]any{"n": float64(1.5), "items": []any{}}); err == nil {
		t.Fatal("fractional number accepted as integer")
	}
	if err := validateSchema(sch, map[string]any{"n": float64(1), "items": []any{"x"}, "tag": "c"}); err == nil {
		t.Fatal("enum accepted an unknown value")
	}
	nullOrString := map[string]any{"type": []any{"string", "null"}}
	if err := validateSchema(nullOrString, nil); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema(nullOrString, "a"); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema(nullOrString, float64(1)); err == nil {
		t.Fatal("number accepted as string or null")
	}
	env := map[string]any{
		"type":                 "object",
		"additionalProperties": map[string]any{"type": "string"},
	}
	if err := validateSchema(env, map[string]any{"TOKEN": "example"}); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema(env, map[string]any{"TOKEN": float64(1)}); err == nil {
		t.Fatal("numeric map value accepted")
	}
	root := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"child": map[string]any{"$ref": "#/$defs/child"},
		},
		"$defs": map[string]any{
			"child": map[string]any{
				"type":     "object",
				"required": []any{"ok"},
				"properties": map[string]any{
					"ok": map[string]any{"type": "boolean"},
				},
			},
		},
	}
	if err := validateSchema(root, map[string]any{"child": map[string]any{"ok": true}}); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema(root, map[string]any{"child": map[string]any{}}); err == nil {
		t.Fatal("$ref did not apply required")
	}
}

func jsonKind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", v)
	}
}
