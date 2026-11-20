package github

import (
	"context"
	"fmt"
)

// StarList is one of the viewer's GitHub Lists.
type StarList struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Private     bool   `json:"private,omitempty"`
}

// StarRepo is one repository the viewer starred or filed in a List.
type StarRepo struct {
	FullName string     `json:"full_name"`
	URL      string     `json:"url"`
	Starred  bool       `json:"starred"`
	Lists    []StarList `json:"lists,omitempty"`
}

// StarSnapshot is the viewer's stars and Lists at one moment. Repos are
// in starred order (newest first), then repos only a List holds, each
// once; Lists keep GitHub's order.
type StarSnapshot struct {
	Viewer string     `json:"viewer"`
	Repos  []StarRepo `json:"repos"`
	Lists  []StarList `json:"lists"`
}

const (
	starredQuery = `query($after: String) {
  viewer {
    login
    starredRepositories(first: 100, after: $after, orderBy: {field: STARRED_AT, direction: DESC}) {
      pageInfo { hasNextPage endCursor }
      nodes { nameWithOwner url }
    }
  }
}`

	listsQuery = `query($after: String) {
  viewer {
    lists(first: 50, after: $after) {
      pageInfo { hasNextPage endCursor }
      nodes {
        id name slug description isPrivate
        items(first: 100) {
          pageInfo { hasNextPage endCursor }
          nodes { ... on Repository { nameWithOwner url } }
        }
      }
    }
  }
}`

	listItemsQuery = `query($id: ID!, $after: String) {
  node(id: $id) {
    ... on UserList {
      items(first: 100, after: $after) {
        pageInfo { hasNextPage endCursor }
        nodes { ... on Repository { nameWithOwner url } }
      }
    }
  }
}`
)

type gqlPage struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type gqlRepo struct {
	NameWithOwner string `json:"nameWithOwner"`
	URL           string `json:"url"`
}

type gqlRepoConn struct {
	PageInfo gqlPage   `json:"pageInfo"`
	Nodes    []gqlRepo `json:"nodes"`
}

// FetchStars reads the viewer's starred repositories and Lists through
// GraphQL, with the token of the account serving owner. The viewer is
// whoever owns that token: GitHub Lists are only readable by their
// owner.
func (c *Client) FetchStars(ctx context.Context, owner string) (*StarSnapshot, error) {
	snap := &StarSnapshot{}
	idx := map[string]int{}
	add := func(r gqlRepo) *StarRepo {
		if r.NameWithOwner == "" {
			return nil // item type other than Repository
		}
		i, ok := idx[r.NameWithOwner]
		if !ok {
			i = len(snap.Repos)
			idx[r.NameWithOwner] = i
			snap.Repos = append(snap.Repos, StarRepo{FullName: r.NameWithOwner, URL: r.URL})
		}
		return &snap.Repos[i]
	}

	after := ""
	for {
		var out struct {
			Viewer struct {
				Login               string      `json:"login"`
				StarredRepositories gqlRepoConn `json:"starredRepositories"`
			} `json:"viewer"`
		}
		if err := c.GraphQL(ctx, owner, starredQuery, map[string]any{"after": nilIfEmpty(after)}, &out); err != nil {
			return nil, fmt.Errorf("github: starred repositories: %w", err)
		}
		snap.Viewer = out.Viewer.Login
		for _, n := range out.Viewer.StarredRepositories.Nodes {
			if r := add(n); r != nil {
				r.Starred = true
			}
		}
		pi := out.Viewer.StarredRepositories.PageInfo
		if !pi.HasNextPage || pi.EndCursor == "" {
			break
		}
		after = pi.EndCursor
	}

	after = ""
	for {
		var out struct {
			Viewer struct {
				Lists struct {
					PageInfo gqlPage `json:"pageInfo"`
					Nodes    []struct {
						ID          string      `json:"id"`
						Name        string      `json:"name"`
						Slug        string      `json:"slug"`
						Description string      `json:"description"`
						IsPrivate   bool        `json:"isPrivate"`
						Items       gqlRepoConn `json:"items"`
					} `json:"nodes"`
				} `json:"lists"`
			} `json:"viewer"`
		}
		if err := c.GraphQL(ctx, owner, listsQuery, map[string]any{"after": nilIfEmpty(after)}, &out); err != nil {
			return nil, fmt.Errorf("github: star lists: %w", err)
		}
		for _, n := range out.Viewer.Lists.Nodes {
			list := StarList{Slug: n.Slug, Name: n.Name, Description: n.Description, Private: n.IsPrivate}
			snap.Lists = append(snap.Lists, list)
			items := n.Items
			for {
				for _, it := range items.Nodes {
					if r := add(it); r != nil {
						r.Lists = append(r.Lists, list)
					}
				}
				if !items.PageInfo.HasNextPage || items.PageInfo.EndCursor == "" {
					break
				}
				var more struct {
					Node struct {
						Items gqlRepoConn `json:"items"`
					} `json:"node"`
				}
				vars := map[string]any{"id": n.ID, "after": items.PageInfo.EndCursor}
				if err := c.GraphQL(ctx, owner, listItemsQuery, vars, &more); err != nil {
					return nil, fmt.Errorf("github: star list %q items: %w", n.Slug, err)
				}
				items = more.Node.Items
			}
		}
		pi := out.Viewer.Lists.PageInfo
		if !pi.HasNextPage || pi.EndCursor == "" {
			break
		}
		after = pi.EndCursor
	}
	return snap, nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
