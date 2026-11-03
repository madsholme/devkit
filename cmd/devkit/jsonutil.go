package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// Compose configuration is handled as generic JSON so that every field docker compose
// produces survives a round trip through the saved compose.json.

func readCompose(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeObject(data)
}

func decodeObject(data []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if value == nil {
		value = map[string]any{}
	}
	return value, nil
}

func writeCompose(path string, value map[string]any) error {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return err
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func asObject(value any) map[string]any {
	m, _ := value.(map[string]any)
	return m
}

// object returns m[key] as an object, or nil.
func object(m map[string]any, key string) map[string]any { return asObject(m[key]) }

// ensureObject returns m[key] as an object, creating it when missing.
func ensureObject(m map[string]any, key string) map[string]any {
	if o := object(m, key); o != nil {
		return o
	}
	o := map[string]any{}
	m[key] = o
	return o
}

func stringMap(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = s
		} else if v != nil {
			out[k] = fmt.Sprint(v)
		}
	}
	return out
}

func str(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// truthy mirrors Python truthiness for JSON values.
func truthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case json.Number:
		return v.String() != "0"
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	}
	return true
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
