package search

import "testing"

func TestDetectQueryMode(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  QueryMode
	}{
		{name: "single keyword", query: "Redis", want: QueryModeKeyword},
		{name: "question with question mark", query: "what are the patterns for distributed transactions?", want: QueryModeQuestion},
		{name: "long question word no mark", query: "what are the patterns for distributed transactions", want: QueryModeQuestion},
		{name: "technical indicators", query: "handleHTTPRequest v2.3 API JWT", want: QueryModeTechnical},
		{name: "default best practices", query: "authentication best practices", want: QueryModeDefault},
		{name: "empty query no panic", query: "", want: QueryModeDefault},
		{name: "trailing question mark", query: "retry?", want: QueryModeQuestion},
		{name: "short two word keyword", query: "go routines", want: QueryModeKeyword},
		{name: "camelCase technical", query: "parseJSONResponse handleError renderTemplate", want: QueryModeTechnical},
		{name: "ALL_CAPS tokens", query: "HTTP_TIMEOUT MAX_RETRIES DB_HOST config", want: QueryModeTechnical},
		// 2 file paths + 1 path-like setup = still only 2 technical tokens → default
		{name: "two file paths default", query: "/etc/nginx/nginx.conf /var/log/app.log setup", want: QueryModeDefault},
		// 2 version tokens, not ≥ 3 → default
		{name: "two version numbers default", query: "upgrade from v1.2 to v3.4 breaking changes", want: QueryModeDefault},
		// 3 file-path tokens → technical
		{name: "three file paths technical", query: "/etc/nginx.conf /var/log/app.log /tmp/data.json", want: QueryModeTechnical},
		// 3 version tokens → technical
		{name: "three version numbers technical", query: "v1.2 v3.4 v5.0 compatibility matrix", want: QueryModeTechnical},
		{name: "is question word long", query: "is there a way to configure the connection pool size", want: QueryModeQuestion},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectQueryMode(tt.query)
			if got != tt.want {
				t.Errorf("DetectQueryMode(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}
