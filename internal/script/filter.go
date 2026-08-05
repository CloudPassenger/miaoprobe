package script

import (
	"fmt"
	"strings"
)

// FilterMode selects whether Select keeps or discards scripts matching a
// FilterSpec's criteria.
type FilterMode string

const (
	// ModeInclude (the default, zero value) keeps scripts that match.
	ModeInclude FilterMode = "include"
	// ModeExclude keeps scripts that do NOT match.
	ModeExclude FilterMode = "exclude"
)

// FilterSpec selects a subset of scripts by exact ID or by
// category/region/tag membership. ID, Category, Region, and Tag are OR'd
// together: a script is considered a "match" if it satisfies at least one
// of them. Mode then decides whether matches are kept (ModeInclude, the
// default) or dropped (ModeExclude). A zero-value FilterSpec has no
// criteria and matches (and keeps) every script.
type FilterSpec struct {
	Mode     FilterMode `koanf:"mode"`
	ID       []string   `koanf:"id"`
	Category []string   `koanf:"category"`
	Region   []string   `koanf:"region"`
	Tag      []string   `koanf:"tag"`
}

// IsZero reports whether spec has no selection criteria, i.e. it matches
// every script regardless of Mode.
func (spec FilterSpec) IsZero() bool {
	return len(spec.ID) == 0 && len(spec.Category) == 0 && len(spec.Region) == 0 && len(spec.Tag) == 0
}

// matches reports whether s satisfies at least one of spec's criteria.
func (spec FilterSpec) matches(s Script) bool {
	for _, id := range spec.ID {
		if strings.EqualFold(s.ID, id) {
			return true
		}
	}
	if s.Category != "" {
		for _, category := range spec.Category {
			if strings.EqualFold(s.Category, category) {
				return true
			}
		}
	}
	for _, region := range s.Regions {
		for _, r := range spec.Region {
			if strings.EqualFold(region, r) {
				return true
			}
		}
	}
	for _, tag := range s.Tags {
		for _, t := range spec.Tag {
			if strings.EqualFold(tag, t) {
				return true
			}
		}
	}
	return false
}

// Select returns the scripts kept by spec, preserving order. With a
// zero-value spec (no criteria set), every script is returned unchanged.
func Select(scripts []Script, spec FilterSpec) []Script {
	if spec.IsZero() {
		return scripts
	}
	exclude := spec.Mode == ModeExclude
	out := make([]Script, 0, len(scripts))
	for _, s := range scripts {
		if spec.matches(s) != exclude {
			out = append(out, s)
		}
	}
	return out
}

// ParseFilterFlag parses the --filter flag's compact DSL: semicolon
// separated "key:v1,v2" segments, e.g.
// "category:media,ai;region:hk,us;id:netflix;mode:exclude". Recognized keys
// are id, category, region, and tag (each a comma-separated list), and mode
// (include or exclude). An empty string returns a zero-value FilterSpec
// (match everything).
func ParseFilterFlag(raw string) (FilterSpec, error) {
	var spec FilterSpec
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return spec, nil
	}
	for _, segment := range strings.Split(raw, ";") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		key, value, ok := strings.Cut(segment, ":")
		if !ok {
			return FilterSpec{}, fmt.Errorf("invalid --filter segment %q: want key:value", segment)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		values := splitCSV(value)
		switch key {
		case "id":
			spec.ID = values
		case "category":
			spec.Category = values
		case "region":
			spec.Region = values
		case "tag":
			spec.Tag = values
		case "mode":
			if len(values) != 1 {
				return FilterSpec{}, fmt.Errorf("invalid --filter mode %q: want a single value", value)
			}
			mode := FilterMode(strings.ToLower(values[0]))
			if mode != ModeInclude && mode != ModeExclude {
				return FilterSpec{}, fmt.Errorf("invalid --filter mode %q: want include or exclude", values[0])
			}
			spec.Mode = mode
		default:
			return FilterSpec{}, fmt.Errorf("unknown --filter key %q: want id, category, region, tag, or mode", key)
		}
	}
	return spec, nil
}

// splitCSV splits a comma-separated string into trimmed, non-empty parts.
func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
