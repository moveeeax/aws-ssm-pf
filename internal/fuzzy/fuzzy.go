// Package fuzzy provides a small subsequence matcher used to filter targets.
package fuzzy

import (
	"sort"
	"strings"
)

// Score returns a match score for query against target and whether target
// contains query as a (case-insensitive) subsequence. Higher scores are better.
// Contiguous runs and matches at word boundaries are rewarded, so "web" ranks
// "web-01" above "wxexbx".
func Score(query, target string) (int, bool) {
	if query == "" {
		return 0, true
	}
	q := strings.ToLower(query)
	t := strings.ToLower(target)

	score := 0
	ti := 0
	prevMatch := -2
	for qi := 0; qi < len(q); qi++ {
		found := -1
		for ; ti < len(t); ti++ {
			if t[ti] == q[qi] {
				found = ti
				break
			}
		}
		if found == -1 {
			return 0, false
		}
		score += 1
		if found == prevMatch+1 {
			score += 5 // contiguous
		}
		if found == 0 || isBoundary(t[found-1]) {
			score += 3 // word boundary
		}
		prevMatch = found
		ti = found + 1
	}
	// Prefer shorter targets when scores are otherwise equal.
	score = score*100 - len(t)
	return score, true
}

func isBoundary(b byte) bool {
	return b == '-' || b == '_' || b == ' ' || b == '.' || b == '/'
}

// Rank returns items matching query, ordered best-first. With an empty query it
// returns items unchanged.
func Rank[T any](items []T, query string, key func(T) string) []T {
	if strings.TrimSpace(query) == "" {
		return items
	}
	type scored struct {
		item  T
		score int
		order int
	}
	var matched []scored
	for idx, it := range items {
		if s, ok := Score(query, key(it)); ok {
			matched = append(matched, scored{it, s, idx})
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].score != matched[j].score {
			return matched[i].score > matched[j].score
		}
		return matched[i].order < matched[j].order
	})
	out := make([]T, len(matched))
	for i, m := range matched {
		out[i] = m.item
	}
	return out
}
