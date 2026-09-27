package grpc

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
)

// Server reflection method names. Reflection is registered only when
// enabled; it still needs a scope whenever auth is on.
const (
	reflectionV1Method      = "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo"
	reflectionV1AlphaMethod = "/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo"
)

// methodScopes is the gRPC method-to-scope table: the same scopes the
// HTTP route table requires for the same operation. A method missing
// from the table is refused (fail closed), and a test fails when a
// registered method has no entry. grpc.health.v1 stays open, like the
// HTTP /health endpoints.
var methodScopes = map[string]authn.Scope{
	pb.AnalyzeService_Analyze_FullMethodName: authn.ScopeWriteObjects,

	pb.JobService_GetJob_FullMethodName:   authn.ScopeReadJobs,
	pb.JobService_ListJobs_FullMethodName: authn.ScopeReadJobs,
	pb.JobService_WatchJob_FullMethodName: authn.ScopeReadJobs,

	pb.QueryService_Search_FullMethodName:          authn.ScopeReadObjects,
	pb.QueryService_ListObjects_FullMethodName:     authn.ScopeReadObjects,
	pb.QueryService_GetObject_FullMethodName:       authn.ScopeReadObjects,
	pb.QueryService_NodeAwareSearch_FullMethodName: authn.ScopeReadObjects,

	pb.EntityService_ListEntities_FullMethodName:       authn.ScopeReadObjects,
	pb.EntityService_GetEntity_FullMethodName:          authn.ScopeReadObjects,
	pb.EntityService_GetEntityBacklinks_FullMethodName: authn.ScopeReadObjects,

	reflectionV1Method:      authn.ScopeReadSystem,
	reflectionV1AlphaMethod: authn.ScopeReadSystem,
}

// authorizeMethod checks that the principal on ctx holds the scope
// fullMethod requires. An unknown method or a missing principal is
// refused. Denials are recorded on the security emitter (nil-safe).
func authorizeMethod(ctx context.Context, fullMethod string, sec *security.Emitter) error {
	p, ok := authn.FromContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "authentication required")
	}
	scope, known := methodScopes[fullMethod]
	if !known {
		if sec != nil {
			sec.RecordACLDenial(ctx, p.ID)
		}
		return status.Errorf(codes.PermissionDenied, "INSUFFICIENT_SCOPE: method %s declares no scope", fullMethod)
	}
	if !p.HasScope(scope) {
		if sec != nil {
			sec.RecordACLDenial(ctx, p.ID)
		}
		return status.Error(codes.PermissionDenied, fmt.Sprintf("INSUFFICIENT_SCOPE: missing scope %s", scope))
	}
	return nil
}

// entityGateFor returns the inbound gate that applies to ctx's
// principal: nil for an admin principal (the instance owner), gate
// otherwise. Mirrors the HTTP entityGate.
func entityGateFor(ctx context.Context, gate *registry.InboundGate) *registry.InboundGate {
	if p, ok := authn.FromContext(ctx); ok && p.BypassesEntityGate() {
		return nil
	}
	return gate
}
