package script

import "strings"

// ParseFilter splits a comma-separated --filter value into lowercase terms.
// An empty string means "no filter" (match everything).
func ParseFilter(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	terms := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			terms = append(terms, p)
		}
	}
	return terms
}

// Matches reports whether s has at least one region or tag in terms (or all
// scripts pass when terms is empty).
func (s Script) Matches(terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	for _, term := range terms {
		for _, region := range s.Regions {
			if strings.EqualFold(region, term) {
				return true
			}
		}
		for _, tag := range s.Tags {
			if strings.EqualFold(tag, term) {
				return true
			}
		}
	}
	return false
}

// Filter returns the subset of scripts matching terms, preserving order.
func Filter(scripts []Script, terms []string) []Script {
	if len(terms) == 0 {
		return scripts
	}
	out := make([]Script, 0, len(scripts))
	for _, s := range scripts {
		if s.Matches(terms) {
			out = append(out, s)
		}
	}
	return out
}
