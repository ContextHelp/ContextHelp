package dpkmsclient_test

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

func hopLines(hops []dpkmsclient.LinkHop) []string {
	out := []string{}
	for _, h := range hops {
		out = append(out, h.From+" "+h.Direction+" "+h.Edge.EdgeType+" "+h.ObjectID)
	}
	return out
}

func TestLinks_CreateListDelete(t *testing.T) {
	srv := startObjects(t)
	ctx := context.Background()
	writer := roleClient(t, srv, dpkmstest.RoleWriter)
	admin := roleClient(t, srv, dpkmstest.RoleAdmin)

	edges, err := writer.CreateLink(ctx, dpkmsclient.LinkCreate{Source: "o-1", Target: "o-2", Type: "extends", Context: "why"})
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 2 || edges[0].EdgeType != "extends" || edges[1].EdgeType != "extended-by" || edges[0].Metadata["context"] != "why" {
		t.Fatalf("created = %+v", edges)
	}
	if _, err := writer.CreateLink(ctx, dpkmsclient.LinkCreate{Source: "o-2", Target: "o-3", Type: "related-to"}); err != nil {
		t.Fatal(err)
	}

	reader := roleClient(t, srv, dpkmstest.RoleReader)
	hops, err := reader.ListLinks(ctx, "o-2", dpkmsclient.LinkQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := hopLines(hops), []string{"o-2 out extended-by o-1", "o-2 out related-to o-3", "o-2 in extends o-1"}; !slices.Equal(got, want) {
		t.Errorf("list = %v, want %v", got, want)
	}
	hops, err = reader.ListLinks(ctx, "o-1", dpkmsclient.LinkQuery{Depth: 3, Direction: "out"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := hopLines(hops), []string{"o-1 out extends o-2", "o-2 out related-to o-3"}; !slices.Equal(got, want) {
		t.Errorf("walk = %v, want %v", got, want)
	}
	if hops[1].Depth != 2 {
		t.Errorf("second hop depth = %d, want 2", hops[1].Depth)
	}

	if _, err := writer.DeleteLinks(ctx, "o-1", "o-2", ""); envelope(t, err).ExitCode != 5 {
		t.Errorf("writer delete: %v, want exit 5", err)
	}
	n, err := admin.DeleteLinks(ctx, "o-1", "o-2", "")
	if err != nil || n != 2 {
		t.Fatalf("admin delete = %d, %v; want 2", n, err)
	}
	n, err = admin.DeleteLinks(ctx, "o-3", "o-2", "related-to")
	if err != nil || n != 1 {
		t.Fatalf("typed delete = %d, %v; want 1", n, err)
	}
	if hops, err := reader.ListLinks(ctx, "o-2", dpkmsclient.LinkQuery{}); err != nil || len(hops) != 0 {
		t.Errorf("after delete: %v %v", hopLines(hops), err)
	}
}

func TestLinks_Errors(t *testing.T) {
	srv := startObjects(t)
	ctx := context.Background()
	writer := roleClient(t, srv, dpkmstest.RoleWriter)

	type errCase struct {
		name string
		err  error
		exit int
	}
	var cases []errCase
	_, err := writer.CreateLink(ctx, dpkmsclient.LinkCreate{Source: "o-1", Target: "o-missing", Type: "extends"})
	cases = append(cases, errCase{"missing target", err, 3})
	_, err = writer.CreateLink(ctx, dpkmsclient.LinkCreate{Source: "o-1", Target: "o-2", Type: "imaginary"})
	cases = append(cases, errCase{"unknown type", err, 2})
	_, err = writer.ListLinks(ctx, "o-missing", dpkmsclient.LinkQuery{})
	cases = append(cases, errCase{"list missing object", err, 3})
	_, err = roleClient(t, srv, "none").CreateLink(ctx, dpkmsclient.LinkCreate{Source: "o-1", Target: "o-2", Type: "extends"})
	cases = append(cases, errCase{"no token", err, 5})
	for _, tc := range cases {
		if e := envelope(t, tc.err); e.ExitCode != tc.exit {
			t.Errorf("%s: exit %d (%v), want %d", tc.name, e.ExitCode, tc.err, tc.exit)
		}
	}
}

// The client's link wire types stay in step with what the dpkms handlers
// decode and encode, field for field.
func TestLinkWireParity(t *testing.T) {
	for _, pair := range [][2]reflect.Type{
		{reflect.TypeFor[dpkmsclient.LinkCreate](), reflect.TypeFor[service.LinkRequest]()},
		{reflect.TypeFor[dpkmsclient.LinkHop](), reflect.TypeFor[service.LinkHop]()},
	} {
		if client, server := jsonFields(pair[0]), jsonFields(pair[1]); !reflect.DeepEqual(client, server) {
			t.Errorf("%s wire fields differ from %s\nclient: %v\nserver: %v", pair[0], pair[1], client, server)
		}
	}
}
