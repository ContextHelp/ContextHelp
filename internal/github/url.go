package github

import (
	"net/url"
	"strings"
)

// ParseRepoURL extracts owner and name from a github.com repository web
// URL (https://github.com/<owner>/<name>, optional trailing slash). Sub-paths,
// .git suffixes and other hosts do not match.
func ParseRepoURL(raw string) (owner, name string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !strings.EqualFold(u.Host, "github.com") {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.HasSuffix(parts[1], ".git") {
		return "", "", false
	}
	return parts[0], parts[1], true
}
