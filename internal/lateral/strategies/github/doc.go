// Package github implements the Tier-A platform-keyed lateral strategies
// rooted at github.com. The package ships three strategies:
//
//   - GitHubStrategy        — parent. Specificity=1. Matches host==github.com
//                             and not the gist subdomain.
//   - GistStrategy          — child. Specificity=2. Matches host==gist.github.com.
//   - SecurityAdvisoryStrategy — child. Specificity=2. Matches the global
//                             advisory database paths and per-repo
//                             /security/advisories/ surfaces.
//
// Decoupling: the package defines its own Fetcher and (later) APIClient
// interfaces and never imports the daemon's HTTP/SDK adapters directly.
// The daemon adapts when it calls Register.
//
// Identity-key extraction: strategies surface the canonical identity key
// (e.g. "@github.user.<login>" or "@github.repo.<owner>/<name>") in the
// emitted Candidate.Preview map under PreviewKeyIdentityKey so the
// substrate's identity resolver can look up canonicals before
// materialization. Substrate's lateral.Candidate has no IdentityKey field
// today; PreviewKeyIdentityKey is the documented contract until it does.
package github
