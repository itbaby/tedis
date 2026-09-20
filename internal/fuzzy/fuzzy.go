// Package fuzzy implements fzf-style subsequence matching with scoring:
// consecutive runs, word boundaries (: - _ . space / start) and early
// matches score higher; matching is case-insensitive.
package fuzzy

import (
	"sort"
	"strings"
)

// Match reports whether s contains all pattern runes in order and returns
// a relevance score (higher = better). Empty pattern matches everything
// with score 0.
func Match(pattern, s string) (int, bool) {
	if pattern == "" {
		return 0, true
	}
	p := []rune(strings.ToLower(pattern))
	t := []rune(strings.ToLower(s))

	score := 0
	pi := 0
	prevHit := -2 // index of previous matched rune (for run detection)
	for ti := 0; ti < len(t) && pi < len(p); ti++ {
		if t[ti] != p[pi] {
			continue
		}
		switch {
		case ti == 0:
			score += 8 // match at the very start
		case ti == prevHit+1:
			score += 6 // consecutive run: strongest signal
		case isBoundary(t[ti-1]):
			score += 4 // after : - _ . space /
		default:
			score++
		}
		if pi == 0 {
			score += 8 - min(ti, 8) // earlier first hit ranks higher
		}
		prevHit = ti
		pi++
	}
	if pi < len(p) {
		return 0, false
	}
	return score, true
}

func isBoundary(r rune) bool {
	switch r {
	case ':', '-', '_', '.', ' ', '/', '@':
		return true
	}
	return false
}

type hit struct {
	s     string
	score int
}

func better(a, b hit) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	return a.s < b.s
}

// rank collects the matching entries, best first (ties alphabetically).
func rank(pattern string, items []string) []hit {
	hits := make([]hit, 0, len(items))
	for _, it := range items {
		if sc, ok := Match(pattern, it); ok {
			hits = append(hits, hit{it, sc})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return better(hits[i], hits[j]) })
	return hits
}

func keys(hits []hit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.s
	}
	return out
}

// Filter returns entries matching pattern sorted by score (desc), then
// alphabetically for ties.
func Filter(pattern string, items []string) []string {
	return keys(rank(pattern, items))
}

// RankTop sorts all matching items by score and keeps at most limit.
// Used by incremental scans: cheap cap that keeps the list bounded.
func RankTop(pattern string, items []string, limit int) []string {
	hits := rank(pattern, items)
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return keys(hits)
}
