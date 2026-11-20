package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// starsServer answers the three star queries from canned pages and
// records the variables of each call.
func starsServer(t *testing.T, calls *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.Unmarshal(b, &req)
		cursor, _ := req.Variables["after"].(string)
		switch {
		case strings.Contains(req.Query, "starredRepositories"):
			*calls = append(*calls, "starred:"+cursor)
			if cursor == "" {
				w.Write([]byte(`{"data":{"viewer":{"login":"octo","starredRepositories":{"pageInfo":{"hasNextPage":true,"endCursor":"c1"},"nodes":[{"nameWithOwner":"a/one","url":"https://github.com/a/one"}]}}}}`))
				return
			}
			w.Write([]byte(`{"data":{"viewer":{"login":"octo","starredRepositories":{"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[{"nameWithOwner":"b/two","url":"https://github.com/b/two"},{"nameWithOwner":"c/three","url":"https://github.com/c/three"}]}}}}`))
		case strings.Contains(req.Query, "viewer"):
			*calls = append(*calls, "lists:"+cursor)
			w.Write([]byte(`{"data":{"viewer":{"lists":{"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[
				{"id":"L1","name":"Go tools","slug":"go-tools","description":"CLI things","isPrivate":false,"items":{"pageInfo":{"hasNextPage":true,"endCursor":"i1"},"nodes":[{"nameWithOwner":"a/one","url":"https://github.com/a/one"}]}},
				{"id":"L2","name":"Empty","slug":"empty","description":null,"isPrivate":true,"items":{"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[]}}]}}}}`))
		default:
			*calls = append(*calls, "items:"+req.Variables["id"].(string)+":"+cursor)
			w.Write([]byte(`{"data":{"node":{"items":{"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[{"nameWithOwner":"b/two","url":"https://github.com/b/two"}]}}}}`))
		}
	}))
}

func TestFetchStars_PagesStarredListsAndListItems(t *testing.T) {
	var calls []string
	srv := starsServer(t, &calls)
	defer srv.Close()
	var slept []time.Duration
	snap, err := newTestClient(srv, &slept).FetchStars(context.Background(), "octo")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Viewer != "octo" {
		t.Errorf("viewer = %q", snap.Viewer)
	}
	var starred []string
	for _, r := range snap.Repos {
		if r.Starred {
			starred = append(starred, r.FullName)
		}
	}
	if !reflect.DeepEqual(starred, []string{"a/one", "b/two", "c/three"}) {
		t.Errorf("starred = %v", starred)
	}
	member := map[string][]string{}
	for _, r := range snap.Repos {
		for _, l := range r.Lists {
			member[r.FullName] = append(member[r.FullName], l.Slug)
		}
	}
	if !reflect.DeepEqual(member, map[string][]string{"a/one": {"go-tools"}, "b/two": {"go-tools"}}) {
		t.Errorf("membership = %v", member)
	}
	if len(snap.Lists) != 2 || snap.Lists[0].Name != "Go tools" || snap.Lists[0].Description != "CLI things" || snap.Lists[1].Slug != "empty" {
		t.Errorf("lists = %+v", snap.Lists)
	}
	want := []string{"starred:", "starred:c1", "lists:", "items:L1:i1"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("calls = %v, want %v", calls, want)
	}
}

func TestFetchStars_ListItemNotStarredStillImported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), "starredRepositories") {
			w.Write([]byte(`{"data":{"viewer":{"login":"o","starredRepositories":{"pageInfo":{"hasNextPage":false},"nodes":[]}}}}`))
			return
		}
		w.Write([]byte(`{"data":{"viewer":{"lists":{"pageInfo":{"hasNextPage":false},"nodes":[{"id":"L","name":"X","slug":"x","items":{"pageInfo":{"hasNextPage":false},"nodes":[{"nameWithOwner":"z/z","url":"https://github.com/z/z"}]}}]}}}}`))
	}))
	defer srv.Close()
	var slept []time.Duration
	snap, err := newTestClient(srv, &slept).FetchStars(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Repos) != 1 || snap.Repos[0].Starred || len(snap.Repos[0].Lists) != 1 {
		t.Errorf("repos = %+v", snap.Repos)
	}
}
