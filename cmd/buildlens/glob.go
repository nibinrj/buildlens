package main

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// globToRegexp translates a glob with "**" into a regular expression over slash-separated paths.
// "**/" matches any number of directories (including none), "*" matches within one path segment,
// "?" matches one character. Go's filepath.Glob has no "**", which report patterns need.
func globToRegexp(glob string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch {
		case strings.HasPrefix(glob[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(glob[i:], "**"):
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, fmt.Errorf("glob %q: %w", glob, err)
	}
	return re, nil
}

// skipDirs are never searched for reports.
var skipDirs = map[string]bool{".git": true, "node_modules": true, ".buildlens": true}

// findReports walks root and returns every file matching one of globs, as slash-separated paths relative to
// root, sorted.
func findReports(root string, globs []string) ([]string, error) {
	var res []*regexp.Regexp
	for _, g := range globs {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		re, err := globToRegexp(g)
		if err != nil {
			return nil, err
		}
		res = append(res, re)
	}

	var found []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		for _, re := range res {
			if re.MatchString(rel) {
				found = append(found, rel)
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("search reports under %s: %w", root, err)
	}
	sort.Strings(found)
	return found, nil
}
