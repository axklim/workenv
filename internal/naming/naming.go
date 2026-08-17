// Package naming derives default identifiers (branch names from issue
// titles, tmux session names) and sanitises user-supplied ones. Nothing is
// encoded in a name any more: the state registry records what belongs
// together.
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

// SessionName builds the tmux session name for a work environment: the
// project and branch, sanitized and joined with a dash. tmux target syntax
// reserves ':' and '.', so unsafe characters become dashes.
func SessionName(project, branch string) string {
	return Sanitize(project) + "-" + Sanitize(branch)
}

// BranchForIssue is the default branch for an issue: the title slug, so the
// branch reads like the work. Only a title with no usable characters falls
// back to issue-N.
func BranchForIssue(num int, title string) string {
	if slug := Slugify(title); slug != "" {
		return slug
	}
	return fmt.Sprintf("issue-%d", num)
}

// PRBranch is the local branch a fork PR is materialised on (its head branch
// does not exist on origin).
func PRBranch(num int) string {
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
