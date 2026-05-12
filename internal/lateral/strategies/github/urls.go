package github

import (
	"net/url"
	"strconv"
	"strings"
)

// parsedURL captures the structural fields probes need from a github URL.
// Fields are zero when the URL doesn't carry that signal (e.g. Number is
// 0 for repo pages).
type parsedURL struct {
	Host   string
	Owner  string
	Repo   string
	Number int    // PR or issue number; 0 if not applicable
	Login  string // for /sponsors/<login> pages
}

// parseGitHubURL extracts structural fields from raw. Returns ok=false
// for non-github hosts or URLs that don't match a recognized github
// shape.
func parseGitHubURL(raw string) (parsedURL, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return parsedURL{}, false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return parsedURL{}, false
	}
	out := parsedURL{Host: host}
	p := strings.Trim(u.Path, "/")
	if p == "" {
		return out, true
	}
	parts := strings.Split(p, "/")

	// /sponsors/<login>
	if parts[0] == "sponsors" && len(parts) >= 2 {
		out.Login = parts[1]
		return out, true
	}

	// reserved → don't pretend it's an owner
	if isReservedTopLevel(parts[0]) {
		return out, true
	}

	// /<owner>
	out.Owner = parts[0]
	if len(parts) == 1 {
		return out, true
	}

	// /<owner>/<repo>
	out.Repo = parts[1]
	if len(parts) < 4 {
		return out, true
	}

	// /<owner>/<repo>/<verb>/<number>
	switch parts[2] {
	case "pull", "pulls":
		if n, err := strconv.Atoi(parts[3]); err == nil {
			out.Number = n
		}
	case "issues":
		if n, err := strconv.Atoi(parts[3]); err == nil {
			out.Number = n
		}
	}
	return out, true
}

// repoURL returns the canonical https URL for owner/repo.
func repoURL(owner, repo string) string {
	return "https://github.com/" + owner + "/" + repo
}

// userURL returns the canonical https URL for login (treats user/org
// uniformly; both surface at /<login>).
func userURL(login string) string {
	return "https://github.com/" + login
}

// sponsorURL returns the canonical https URL for /sponsors/<login>.
func sponsorURL(login string) string {
	return "https://github.com/sponsors/" + login
}

// gistURL returns the canonical https URL for a gist.
func gistURL(owner, id string) string {
	return "https://gist.github.com/" + owner + "/" + id
}

// repoIdentityKey returns the canonical identity key for a repo. Used by
// the identity resolver to dedup against pre-existing canonicals.
func repoIdentityKey(owner, repo string) string {
	return "@github.repo." + owner + "/" + repo
}

// userIdentityKey returns the canonical identity key for a user/org.
func userIdentityKey(login string) string {
	return "@github.user." + login
}

// orgIdentityKey returns the canonical identity key for an org.
func orgIdentityKey(login string) string {
	return "@github.org." + login
}
