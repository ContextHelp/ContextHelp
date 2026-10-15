package search

import (
	"reflect"
	"strings"
	"testing"
)

// The fields share policies filter by (ADR-080 §2): profile (an alias of
// profile_id), class (membership of the classes JSON array) and
// metadata.<key> (a bound JSON path), compiled for both dialects with
// every value bound.
func TestCompilePolicyFields(t *testing.T) {
	for _, tc := range []struct {
		name     string
		query    string
		sqlite   string
		postgres string
		args     []any
		// pgArgs replaces args for Postgres when the two dialects bind
		// different values (a metadata key's JSON path).
		pgArgs []any
	}{
		{
			name:     "profile aliases profile_id",
			query:    "profile==personal",
			sqlite:   "profile_id = ?",
			postgres: "profile_id = $1",
			args:     []any{"personal"},
		},
		{
			name:     "profile in",
			query:    "profile=in=(work,client)",
			sqlite:   "profile_id IN (?,?)",
			postgres: "profile_id IN ($1,$2)",
			args:     []any{"work", "client"},
		},
		{
			name:     "class equals",
			query:    "class==personal",
			sqlite:   "EXISTS (SELECT 1 FROM json_each(classes) WHERE json_each.value IN (?))",
			postgres: "EXISTS (SELECT 1 FROM jsonb_array_elements_text(classes) AS c WHERE c IN ($1))",
			args:     []any{"personal"},
		},
		{
			name:     "class not equals",
			query:    "class!=personal",
			sqlite:   "NOT EXISTS (SELECT 1 FROM json_each(classes) WHERE json_each.value IN (?))",
			postgres: "NOT EXISTS (SELECT 1 FROM jsonb_array_elements_text(classes) AS c WHERE c IN ($1))",
			args:     []any{"personal"},
		},
		{
			name:     "class out",
			query:    "class=out=(personal,financial)",
			sqlite:   "NOT EXISTS (SELECT 1 FROM json_each(classes) WHERE json_each.value IN (?,?))",
			postgres: "NOT EXISTS (SELECT 1 FROM jsonb_array_elements_text(classes) AS c WHERE c IN ($1,$2))",
			args:     []any{"personal", "financial"},
		},
		{
			name:     "class in",
			query:    "class=in=(personal,financial)",
			sqlite:   "EXISTS (SELECT 1 FROM json_each(classes) WHERE json_each.value IN (?,?))",
			postgres: "EXISTS (SELECT 1 FROM jsonb_array_elements_text(classes) AS c WHERE c IN ($1,$2))",
			args:     []any{"personal", "financial"},
		},
		{
			name:     "metadata key",
			query:    "metadata.city==Lisbon",
			sqlite:   "json_extract(metadata, ?) = ?",
			postgres: "(metadata #>> $1::text[]) = $2",
			args:     []any{"$.city", "Lisbon"},
			pgArgs:   []any{"{city}", "Lisbon"},
		},
		{
			name:     "metadata nested key in",
			query:    "metadata.enrichment.structured_metadata.type=in=(person_note,idea)",
			sqlite:   "json_extract(metadata, ?) IN (?,?)",
			postgres: "(metadata #>> $1::text[]) IN ($2,$3)",
			args:     []any{"$.enrichment.structured_metadata.type", "person_note", "idea"},
			pgArgs:   []any{"{enrichment,structured_metadata,type}", "person_note", "idea"},
		},
		{
			name:     "metadata key not equals",
			query:    "metadata.kind!=draft",
			sqlite:   "json_extract(metadata, ?) != ?",
			postgres: "(metadata #>> $1::text[]) != $2",
			args:     []any{"$.kind", "draft"},
			pgArgs:   []any{"{kind}", "draft"},
		},
		{
			name:     "policy data with exclusion",
			query:    "type==contact;tag=in=(work,client);class=out=(personal)",
			sqlite:   "(type = ?) AND (EXISTS (SELECT 1 FROM json_each(tags) WHERE json_each.value->>'label' IN (?,?))) AND (NOT EXISTS (SELECT 1 FROM json_each(classes) WHERE json_each.value IN (?)))",
			postgres: "(type = $1) AND (EXISTS (SELECT 1 FROM jsonb_array_elements(tags) WHERE value->>'label' IN ($2,$3))) AND (NOT EXISTS (SELECT 1 FROM jsonb_array_elements_text(classes) AS c WHERE c IN ($4)))",
			args:     []any{"contact", "work", "client", "personal"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node, err := Parse(tc.query)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			pgArgs := tc.pgArgs
			if pgArgs == nil {
				pgArgs = tc.args
			}
			for _, d := range []struct {
				dialect  Dialect
				want     string
				wantArgs []any
			}{{DialectSQLite, tc.sqlite, tc.args}, {DialectPostgres, tc.postgres, pgArgs}} {
				sql, args, err := CompileFor(d.dialect, node)
				if err != nil {
					t.Fatalf("CompileFor(%v): %v", d.dialect, err)
				}
				if sql != d.want {
					t.Errorf("CompileFor(%v) = %q, want %q", d.dialect, sql, d.want)
				}
				if !reflect.DeepEqual(args, d.wantArgs) {
					t.Errorf("CompileFor(%v) args = %v, want %v", d.dialect, args, d.wantArgs)
				}
			}
		})
	}
}

// Postgres placeholders are numbered after the path argument, so a
// metadata comparison's key lands in $1 and its value in $2.
func TestCompilePolicyFields_PostgresArgOrder(t *testing.T) {
	node, err := Parse("metadata.city==Lisbon;profile==work")
	if err != nil {
		t.Fatal(err)
	}
	sql, args, err := CompileFor(DialectPostgres, node)
	if err != nil {
		t.Fatal(err)
	}
	if want := "((metadata #>> $1::text[]) = $2) AND (profile_id = $3)"; sql != want {
		t.Errorf("sql = %q, want %q", sql, want)
	}
	if want := []any{"{city}", "Lisbon", "work"}; !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

// Nested metadata keys compile to a Postgres path array.
func TestCompilePolicyFields_PostgresNestedPath(t *testing.T) {
	node, err := Parse("metadata.a.b==1")
	if err != nil {
		t.Fatal(err)
	}
	_, args, err := CompileFor(DialectPostgres, node)
	if err != nil {
		t.Fatal(err)
	}
	if args[0] != "{a,b}" {
		t.Errorf("path arg = %v, want {a,b}", args[0])
	}
}

func TestCompilePolicyFields_Refusals(t *testing.T) {
	for _, tc := range []struct {
		name  string
		node  Node
		wants string
	}{
		{"class with a range operator", ComparisonNode{Field: "class", Operator: OpGt, Value: "a"}, "unsupported operator"},
		{"class equals with a list", ComparisonNode{Field: "class", Operator: OpEq, Value: []string{"a"}}, "string value"},
		{"class in with a string", ComparisonNode{Field: "class", Operator: OpIn, Value: "a"}, "[]string value"},
		{"class in with nothing", ComparisonNode{Field: "class", Operator: OpIn, Value: []string{}}, "at least one class"},
		{"metadata key with a quote", ComparisonNode{Field: "metadata.a'b", Operator: OpEq, Value: "x"}, "metadata key"},
		{"metadata without a key", ComparisonNode{Field: "metadata.", Operator: OpEq, Value: "x"}, "metadata key"},
		{"unknown field stays unknown", ComparisonNode{Field: "topic", Operator: OpEq, Value: "x"}, "unknown field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, d := range []Dialect{DialectSQLite, DialectPostgres} {
				_, _, err := CompileFor(d, tc.node)
				if err == nil || !strings.Contains(err.Error(), tc.wants) {
					t.Errorf("CompileFor(%v) err = %v, want one naming %q", d, err, tc.wants)
				}
			}
		})
	}
}
