//go:build integration

package postgres_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TestSharingPredicate_RunsOnPostgres compiles viewers from the ADR-080
// templates and runs their Postgres predicates through ListBySQL on a
// seeded store: the union of two grants, the ceiling and the key policy
// each select the rows the SQLite cases select.
func TestSharingPredicate_RunsOnPostgres(t *testing.T) {
	ctx := context.Background()
	drv, _ := freshIntegrationDriver(t)
	now := time.Now().UTC().Truncate(time.Second)
	for _, o := range []struct {
		name, typ, tag string
		classes        []string
	}{
		{"c1", "contact", "work", nil},
		{"c2", "contact", "family", nil},
		{"c3", "contact", "client", []string{"personal"}},
		{"c4", "contact", "work", []string{"financial"}},
		{"n1", "note", "work", nil},
	} {
		if err := drv.Objects().Create(ctx, &storage.KnowledgeObject{
			ID: "obj-" + o.name, Type: o.typ, RawContent: o.name, Status: storage.ObjectStatusActive,
			Tags: []storage.Tag{{Label: o.tag}}, Classes: o.classes, ContentHash: "sha256:" + o.name,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed %s: %v", o.name, err)
		}
	}
	colleagues, _ := sharing.Template(sharing.TemplateColleagues)
	partners, _ := sharing.Template(sharing.TemplatePartners)
	col := sharing.Grant{ID: "grt_1", Audience: "colleagues", Policy: colleagues}
	par := sharing.Grant{ID: "grt_2", Audience: "partners", Policy: partners}
	for _, tc := range []struct {
		name string
		in   sharing.Input
		want []string
	}{
		{"colleagues alone", sharing.Input{PrincipalID: "p", Grants: []sharing.Grant{col}}, []string{"obj-c1", "obj-c4"}},
		{"union of two grants", sharing.Input{PrincipalID: "p", Grants: []sharing.Grant{col, par}}, []string{"obj-c1", "obj-c2", "obj-c4"}},
		{"ceiling removes financial", sharing.Input{PrincipalID: "p", Grants: []sharing.Grant{col, par}, DenyClasses: []string{"financial"}}, []string{"obj-c1", "obj-c2"}},
		{"key policy narrows", sharing.Input{PrincipalID: "p", Grants: []sharing.Grant{par}, KeyPolicy: &colleagues}, []string{"obj-c1", "obj-c4"}},
		{"no grants", sharing.Input{PrincipalID: "p"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := sharing.Compile(tc.in)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			sql, args := v.Predicate(search.DialectPostgres)
			objs, total, err := drv.Objects().ListBySQL(ctx, sql, args, storage.ObjectFilter{Status: storage.ObjectStatusAll})
			if err != nil {
				t.Fatalf("ListBySQL(%q): %v", sql, err)
			}
			got := make([]string, len(objs))
			for i, o := range objs {
				got[i] = o.ID
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) || total != len(tc.want) {
				t.Fatalf("visible = %v (total %d), want %v\nsql: %s\nargs: %v", got, total, tc.want, sql, args)
			}
		})
	}
}
