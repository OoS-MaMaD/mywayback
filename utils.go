package main

import (
	"regexp"
	"sort"
	"strings"
)

// CompileExtRegex returns regex and includeMode
func CompileExtRegex(includeCSV, excludeCSV string) (*regexp.Regexp, bool, error) {
	includeMode := false
	csv := excludeCSV
	if strings.TrimSpace(includeCSV) != "" {
		includeMode = true
		csv = includeCSV
	}
	parts := []string{}
	for _, p := range strings.Split(csv, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = strings.TrimPrefix(p, ".")
		parts = append(parts, regexp.QuoteMeta(p))
	}
	if len(parts) == 0 {
		return nil, includeMode, nil
	}
	reStr := `(?i)\.(` + strings.Join(parts, "|") + `)$`
	re, err := regexp.Compile(reStr)
	return re, includeMode, err
}

// uniqueAndSort removes duplicates and sorts
func uniqueAndSort(lines []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, l := range lines {
		if l == "" {
			continue
		}
		if _, ok := seen[l]; ok {
			continue
		}
		seen[l] = struct{}{}
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}
