package grpc

import (
	"maps"
	"testing"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
)

// MethodScopes hands out the table, never the table itself: a caller
// that edits its copy leaves the scopes the interceptor enforces alone.
func TestMethodScopesIsACopyOfTheTable(t *testing.T) {
	got := MethodScopes()
	if !maps.Equal(got, methodScopes) {
		t.Fatalf("MethodScopes() = %v, want the table %v", got, methodScopes)
	}
	got[pb.AnalyzeService_Analyze_FullMethodName] = authn.ScopeReadObjects
	if s := methodScopes[pb.AnalyzeService_Analyze_FullMethodName]; s != authn.ScopeWriteObjects {
		t.Fatalf("editing the copy changed the table: Analyze now %s", s)
	}
}
