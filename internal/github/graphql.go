package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// graphqlError is one entry of a GraphQL response's "errors" array.
type graphqlError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type graphqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphqlError  `json:"errors"`
}

// GraphQL posts query to the GraphQL API on behalf of owner (the token
// source picks the account serving that owner) and decodes "data" into
// out. It shares the REST rate-limit handling: a RATE_LIMITED answer
// waits for the reset or fails with a *RateLimitError. Any other GraphQL
// error fails the call with its messages.
func (c *Client) GraphQL(ctx context.Context, owner, query string, vars map[string]any, out any) error {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return fmt.Errorf("github: encode graphql request: %w", err)
	}
	body, err := c.do(ctx, "POST", graphqlURL(c.baseURL), owner, "", payload)
	if err != nil {
		return err
	}
	var resp graphqlResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("github: decode graphql response: %w", err)
	}
	if len(resp.Errors) > 0 {
		msgs := make([]string, len(resp.Errors))
		for i, e := range resp.Errors {
			msgs[i] = e.Message
		}
		return fmt.Errorf("github: graphql: %s", strings.Join(msgs, "; "))
	}
	if out == nil || len(resp.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(resp.Data, out); err != nil {
		return fmt.Errorf("github: decode graphql data: %w", err)
	}
	return nil
}

// graphqlURL derives the GraphQL endpoint from the REST root: GitHub.com
// serves it at /graphql, Enterprise Server at /api/graphql beside
// /api/v3.
func graphqlURL(base string) string {
	if root, ok := strings.CutSuffix(base, "/api/v3"); ok {
		return root + "/api/graphql"
	}
	return base + "/graphql"
}

// graphqlRateLimited reports whether a 200 body carries GitHub's
// GraphQL rate-limit error (GraphQL limits answer 200, not 403).
func graphqlRateLimited(body []byte) bool {
	var resp graphqlResponse
	if json.Unmarshal(body, &resp) != nil {
		return false
	}
	for _, e := range resp.Errors {
		if e.Type == "RATE_LIMITED" {
			return true
		}
	}
	return false
}
