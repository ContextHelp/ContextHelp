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

// methodAccess says what a restricted viewer needs to call a method
// and what the method's answer carries (ADR-080 §8: the response
// shape the visibility backstop walks).
type methodAccess struct {
	ownerOnly  bool
	capability string
	shape      sharing.Shape
}

// methodAccessTable classifies every method of methodScopes; the
// package's tests check the two tables name the same methods, and an
// unclassified method fails closed at runtime.
var methodAccessTable = map[string]methodAccess{
	pb.AnalyzeService_Analyze_FullMethodName: {shape: sharing.ShapeNone},

	pb.JobService_GetJob_FullMethodName:   {ownerOnly: true, shape: sharing.ShapeNone},
	pb.JobService_ListJobs_FullMethodName: {ownerOnly: true, shape: sharing.ShapeNone},
	pb.JobService_WatchJob_FullMethodName: {ownerOnly: true, shape: sharing.ShapeNone},

	pb.QueryService_Search_FullMethodName:          {capability: storage.CapabilitySearchPrefix + sharing.SearchRSQL, shape: sharing.ShapeObjects},
	pb.QueryService_ListObjects_FullMethodName:     {shape: sharing.ShapeObjects},
	pb.QueryService_GetObject_FullMethodName:       {shape: sharing.ShapeObjects},
	pb.QueryService_NodeAwareSearch_FullMethodName: {capability: storage.CapabilitySearchPrefix + sharing.SearchRSQL, shape: sharing.ShapeObjects},

	pb.EntityService_ListEntities_FullMethodName:       {capability: storage.CapabilityEntities, shape: sharing.ShapeEntities},
	pb.EntityService_GetEntity_FullMethodName:          {capability: storage.CapabilityEntities, shape: sharing.ShapeEntities},
	pb.EntityService_GetEntityBacklinks_FullMethodName: {capability: storage.CapabilityEntities, shape: sharing.ShapeObjects},

	reflectionV1Method:      {shape: sharing.ShapeNone},
	reflectionV1AlphaMethod: {shape: sharing.ShapeNone},
}

// MethodAccess reports what a restricted viewer needs to call
// fullMethod: whether it is owner-only, the capability it needs (""
// for none), and whether the method is classified at all.
func MethodAccess(fullMethod string) (ownerOnly bool, capability string, known bool) {
	a, ok := methodAccessTable[fullMethod]
	return a.ownerOnly, a.capability, ok
}

// MethodShape reports the response shape fullMethod declares, and
// whether the method is classified at all.
func MethodShape(fullMethod string) (sharing.Shape, bool) {
	a, ok := methodAccessTable[fullMethod]
	return a.shape, ok
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
