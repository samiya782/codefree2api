package srd

import (
	"encoding/json"
	"regexp"
	"strings"
)

var semverRE = regexp.MustCompile(`^\d+\.\d+\.\d+`)

// LatestVersion walks JSON and returns the latestVersion string.
func LatestVersion(body []byte) string {
	v, ok := decodeAny(body)
	if !ok {
		return ""
	}
	return walkLatest(v)
}

func walkLatest(v any) string {
	switch t := v.(type) {
	case map[string]any:
		if s := scalar(t["latestVersion"]); s != "" {
			return s
		}
		for _, child := range t {
			if s := walkLatest(child); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range t {
			if s := walkLatest(child); s != "" {
				return s
			}
		}
	}
	return ""
}

// SpaceVersion reads a release list. It accepts version, artifactVersion,
// ideVersion, latestVersion, or a semver name, including a {data:[...]} wrap.
func SpaceVersion(body []byte) string {
	v, ok := decodeAny(body)
	if !ok {
		return ""
	}
	return spaceVersion(v)
}

func spaceVersion(v any) string {
	switch t := v.(type) {
	case []any:
		for _, item := range t {
			if s := versionFields(item); s != "" {
				return s
			}
		}
	case map[string]any:
		if s := versionFields(t); s != "" {
			return s
		}
		for _, key := range []string{"data", "list", "items", "releases"} {
			if s := spaceVersion(t[key]); s != "" {
				return s
			}
		}
	}
	return ""
}

func versionFields(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"version", "artifactVersion", "ideVersion", "latestVersion", "name"} {
		s := scalar(m[key])
		if semverRE.MatchString(s) {
			return s
		}
	}
	return ""
}

// ModelIDs reads a model list from an array or data/models/list/items.
func ModelIDs(body []byte) []string {
	v, ok := decodeAny(body)
	if !ok {
		return nil
	}
	return modelIDs(v, true)
}

func modelIDs(v any, root bool) []string {
	switch t := v.(type) {
	case []any:
		var out []string
		for _, item := range t {
			switch child := item.(type) {
			case string:
				if s := strings.TrimSpace(child); s != "" {
					out = append(out, s)
				}
			case map[string]any:
				if s := modelName(child); s != "" {
					out = append(out, s)
				}
			}
		}
		return dedupe(out)
	case map[string]any:
		if root {
			for _, key := range []string{"data", "models", "list", "items"} {
				if ids := modelIDs(t[key], false); len(ids) > 0 {
					return ids
				}
			}
		}
		if s := modelName(t); s != "" {
			return []string{s}
		}
	case string:
		if s := strings.TrimSpace(t); s != "" {
			return []string{s}
		}
	}
	return nil
}

func modelName(m map[string]any) string {
	for _, key := range []string{"id", "name", "model", "modelName"} {
		if s := scalar(m[key]); s != "" {
			return s
		}
	}
	return ""
}

func dedupe(ids []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func decodeAny(body []byte) (any, bool) {
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	return v, true
}

// Project reads a project lookup. Missing fields are zero and empty.
func Project(body []byte) (int64, string) {
	v, ok := decodeAny(body)
	if !ok {
		return 0, ""
	}
	if id, name, ok := projectFields(v); ok {
		return id, name
	}
	if m, ok := v.(map[string]any); ok {
		for _, key := range []string{"project", "data"} {
			if id, name, ok := projectFields(m[key]); ok {
				return id, name
			}
			if child, ok := m[key].(map[string]any); ok {
				if id, name, ok := projectFields(child["project"]); ok {
					return id, name
				}
			}
		}
	}
	return 0, ""
}

func projectFields(v any) (int64, string, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return 0, "", false
	}
	_, hasID := m["id"]
	_, hasName := m["name"]
	if !hasID && !hasName {
		return 0, "", false
	}
	return number(m["id"]), scalar(m["name"]), true
}

func number(v any) int64 {
	switch t := v.(type) {
	case json.Number:
		n, err := t.Int64()
		if err != nil {
			return 0
		}
		return n
	case string:
		n, err := json.Number(strings.TrimSpace(t)).Int64()
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}
