package grpc

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Capability-closed RPCs (ADR-080 §4, §6; plan decision D19), the gRPC
// half of the HTTP route access table: the job service is owner-only
// (job payloads are raw content), the entity service needs the
// entities capability, the RSQL searches need the rsql search mode,
// and the rest is open, its rows scoped by the viewer's predicate.

// methodAccess says what a restricted viewer needs to call a method.
type methodAccess struct {
	ownerOnly  bool
	capability string
}

// methodAccessTable classifies every method of methodScopes; the
// package's tests check the two tables name the same methods, and an
// unclassified method fails closed at runtime.
var methodAccessTable = map[string]methodAccess{
	pb.AnalyzeService_Analyze_FullMethodName: {},

	pb.JobService_GetJob_FullMethodName:   {ownerOnly: true},
	pb.JobService_ListJobs_FullMethodName: {ownerOnly: true},
	pb.JobService_WatchJob_FullMethodName: {ownerOnly: true},

	pb.QueryService_Search_FullMethodName:          {capability: storage.CapabilitySearchPrefix + sharing.SearchRSQL},
	pb.QueryService_ListObjects_FullMethodName:     {},
	pb.QueryService_GetObject_FullMethodName:       {},
	pb.QueryService_NodeAwareSearch_FullMethodName: {capability: storage.CapabilitySearchPrefix + sharing.SearchRSQL},

	pb.EntityService_ListEntities_FullMethodName:       {capability: storage.CapabilityEntities},
	pb.EntityService_GetEntity_FullMethodName:          {capability: storage.CapabilityEntities},
	pb.EntityService_GetEntityBacklinks_FullMethodName: {capability: storage.CapabilityEntities},

	reflectionV1Method:      {},
	reflectionV1AlphaMethod: {},
}

// MethodAccess reports what a restricted viewer needs to call
// fullMethod: whether it is owner-only, the capability it needs (""
// for none), and whether the method is classified at all.
func MethodAccess(fullMethod string) (ownerOnly bool, capability string, known bool) {
	a, ok := methodAccessTable[fullMethod]
	return a.ownerOnly, a.capability, ok
}

// authorizeCapability checks the viewer on ctx may call fullMethod: an
// unrestricted viewer always; a restricted one when the method is open
// or needs a capability it holds. Anything else, a missing viewer and
// an unclassified method included, is PermissionDenied NOT_SHARED,
// which names no row.
func authorizeCapability(ctx context.Context, fullMethod string) error {
	v, ok := sharing.FromContext(ctx)
	if ok && v.IsUnrestricted() {
		return nil
	}
	access, known := methodAccessTable[fullMethod]
	if !ok || !known || access.ownerOnly || (access.capability != "" && !v.Allows(access.capability)) {
		return status.Error(codes.PermissionDenied, "NOT_SHARED: not shared with this principal")
	}
	return nil
}
