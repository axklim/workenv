// Package naming derives stateless identifiers (branch names, tmux session
// names) so that a work environment can always be re-discovered from its
// name alone, without any state storage.
package naming

import (
	"fmt"
	"strings"
)

// slugMax caps slugs so branch/session names stay readable.
const slugMax = 41

// Slugify converts an arbitrary string (e.g. an issue title) into a short,
// lowercase, dash-separated slug safe for branch and session names.
func Slugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	slug := collapseDashes(b.String())
	if len(slug) > slugMax {
		slug = strings.Trim(slug[:slugMax], "-")
	}
	return slug
}

// SessionName builds the tmux session name for a work environment.
// tmux target syntax reserves ':' and '.', so all unsafe characters are
// normalized to dashes.
func SessionName(project, name string) string {
	return "we-" + Sanitize(project) + "-" + Sanitize(name)
}

// BranchForIssue encodes the issue number into the branch name; this is what
// keeps the program stateless (the number is recoverable from the name).
func BranchForIssue(num int, title string) string {
	if slug := Slugify(title); slug != "" {
		return fmt.Sprintf("issue-%d-%s", num, slug)
	}
	return fmt.Sprintf("issue-%d", num)
}

// PRName is the work-environment name for a pull request checkout.
func PRName(num int) string {
	return fmt.Sprintf("pr-%d", num)
}

func Sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return collapseDashes(b.String())
}

func collapseDashes(s string) string {
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}
