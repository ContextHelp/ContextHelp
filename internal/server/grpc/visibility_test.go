package grpc

import (
	"context"
	"slices"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
)

// Every method declares one of the known shapes.
func TestEveryMethodDeclaresAShape(t *testing.T) {
	for m := range methodScopes {
		shape, known := MethodShape(m)
		if !known || !slices.Contains(sharing.Shapes, shape) {
			t.Errorf("%s declares shape %q (known %v)", m, shape, known)
		}
	}
	if s, _ := MethodShape(pb.QueryService_ListObjects_FullMethodName); s != sharing.ShapeObjects {
		t.Errorf("ListObjects shape %q", s)
	}
	if s, _ := MethodShape(pb.EntityService_ListEntities_FullMethodName); s != sharing.ShapeEntities {
		t.Errorf("ListEntities shape %q", s)
	}
	if s, _ := MethodShape(pb.JobService_GetJob_FullMethodName); s != sharing.ShapeNone {
		t.Errorf("GetJob shape %q", s)
	}
}

func restrictedCtx(t *testing.T) context.Context {
	t.Helper()
	tpl, _ := sharing.Template(sharing.TemplateColleagues)
	v, err := sharing.Compile(sharing.Input{PrincipalID: "prn_vis", Grants: []sharing.Grant{{ID: "g", Audience: "a", Policy: tpl}}})
	if err != nil {
		t.Fatal(err)
	}
	return sharing.WithViewer(context.Background(), v)
}

// The interceptor strips an unreleased object from a listing, refuses
// it strictly, keeps released ones, and leaves the owner alone.
func TestVisibilityInterceptorStripsOrRefuses(t *testing.T) {
	leak := func(ctx context.Context, _ any) (any, error) {
		sharing.LedgerFrom(ctx).AddObject("a")
		return &pb.ListObjectsResponse{Objects: []*pb.KnowledgeObject{{Id: "a", RawContent: "fine"}, {Id: "hidden", RawContent: "secret"}}, Total: 2}, nil
	}
	info := unaryInfo(pb.QueryService_ListObjects_FullMethodName)

	resp, err := visibilityUnaryInterceptor(visibilityConfig{})(restrictedCtx(t), nil, info, leak)
	if err != nil {
		t.Fatalf("lenient: %v", err)
	}
	out := resp.(*pb.ListObjectsResponse)
	if len(out.Objects) != 1 || out.Objects[0].Id != "a" || out.Total != 2 {
		t.Errorf("lenient: got %v", out)
	}

	_, err = visibilityUnaryInterceptor(visibilityConfig{strict: true})(restrictedCtx(t), nil, info, leak)
	if st, _ := status.FromError(err); st.Code() != codes.Internal {
		t.Errorf("strict: err %v, want Internal", err)
	}

	resp, err = visibilityUnaryInterceptor(visibilityConfig{strict: true})(sharing.WithViewer(context.Background(), sharing.Unrestricted), nil, info, leak)
	if err != nil || len(resp.(*pb.ListObjectsResponse).Objects) != 2 {
		t.Errorf("owner: resp %v err %v, want untouched", resp, err)
	}

	absent := func(context.Context, any) (any, error) {
		return &pb.KnowledgeObject{Id: "hidden"}, nil
	}
	_, err = visibilityUnaryInterceptor(visibilityConfig{})(restrictedCtx(t), nil, unaryInfo(pb.QueryService_GetObject_FullMethodName), absent)
	if st, _ := status.FromError(err); st.Code() != codes.NotFound {
		t.Errorf("absent root: err %v, want NotFound", err)
	}

	_, err = visibilityUnaryInterceptor(visibilityConfig{})(restrictedCtx(t), nil, unaryInfo("/no.Such/Method"), leak)
	if st, _ := status.FromError(err); st.Code() != codes.Internal {
		t.Errorf("unclassified: err %v, want Internal", err)
	}
}

// A withheld value in a message is replaced by its category token.
func TestVisibilityInterceptorCanary(t *testing.T) {
	handler := func(ctx context.Context, _ any) (any, error) {
		l := sharing.LedgerFrom(ctx)
		l.AddObject("a")
		l.AddWithheld([]sharing.Withheld{{Category: "phone", Value: "+1 555 0100 2001"}})
		return &pb.KnowledgeObject{Id: "a", RawContent: "call +1 555 0100 2001"}, nil
	}
	resp, err := visibilityUnaryInterceptor(visibilityConfig{})(restrictedCtx(t), nil, unaryInfo(pb.QueryService_GetObject_FullMethodName), handler)
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.(*pb.KnowledgeObject).RawContent; got != "call [phone]" {
		t.Errorf("got %q", got)
	}
}
