package grpc

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
)

// viewAsTarget is the x-ctxt-view-as metadata of the call, trimmed.
func viewAsTarget(ctx context.Context) string {
	if md, found := metadata.FromIncomingContext(ctx); found {
		if vals := md.Get(sharing.MetadataViewAs); len(vals) > 0 {
			return strings.TrimSpace(vals[0])
		}
	}
	return ""
}

// audited reports whether the call is audited (ADR-080 §9): a
// restricted viewer's, or the owner's preview of another viewer.
func audited(ctx context.Context) bool {
	v, ok := sharing.FromContext(ctx)
	return !ok || !v.IsUnrestricted() || viewAsTarget(ctx) != ""
}

// callerRestricted reports whether the caller on ctx is a restricted
// viewer, as opposed to the owner previewing another viewer.
func callerRestricted(ctx context.Context) bool {
	v, ok := sharing.FromContext(ctx)
	return !ok || !v.IsUnrestricted()
}

// ensureReceipt returns ctx with the receipt of the call, starting it
// when no earlier interceptor did (the auth interceptor starts it ahead
// of the scope check, the visibility interceptor otherwise). req is the
// request message of a unary call, nil for a stream, whose messages the
// handler reads itself: a stream's digest covers the method alone.
func ensureReceipt(ctx context.Context, fullMethod string, req any) (context.Context, *sharing.Receipt) {
	if r := sharing.ReceiptFrom(ctx); r != nil {
		return ctx, r
	}
	v, _ := sharing.FromContext(ctx)
	op := sharing.Operation{
		Route: fullMethod, Params: protoParams(req), Viewer: v,
		Principal: v.PrincipalID(), Identity: v.IdentityID(),
	}
	if p, ok := authn.FromContext(ctx); ok {
		op.Principal, op.Identity = p.ID, p.Meta[authn.MetaIdentityID]
		if p.Provider != "" && p.Provider != "none" {
			op.AuthMethod = p.Provider
		}
	}
	r := sharing.NewReceipt(op)
	return sharing.WithReceipt(ctx, r), r
}

// finishReceipt hands the call's receipt to w. The owner's own refused
// calls are not logged (restricted false); a refused preview is one.
// An elevated call's entry is committed durably before it returns, and
// its error, when it could not be, is returned: the caller then answers
// AUDIT_UNAVAILABLE.
func finishReceipt(ctx context.Context, w *sharing.AuditWriter, restricted bool) error {
	r := sharing.ReceiptFrom(ctx)
	if !restricted {
		r.Discard()
	}
	return r.Commit(ctx, w)
}

// maxDigestString is the longest string value the operation digest
// takes: a longer one is body-like, and REST's digest covers no body.
const maxDigestString = 256

// bodyFields are the request fields that carry a body, by name: left out
// of the digest whatever their size, as REST's digest leaves the body
// out (digest version v1 covers no request body).
var bodyFields = map[protoreflect.Name]bool{
	"content": true, "body": true, "raw_content": true, "text_content": true,
}

// bodyLike reports whether the field is left out of the operation
// digest: a bytes field, a field named in bodyFields, or a string
// longer than maxDigestString.
func bodyLike(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
	switch {
	case fd.Kind() == protoreflect.BytesKind, bodyFields[fd.Name()]:
		return true
	case fd.Kind() == protoreflect.StringKind && !fd.IsList() && !fd.IsMap():
		return len(v.String()) > maxDigestString
	}
	return false
}

// protoParams lists the populated fields of a request message for the
// operation digest, namespaced "req.<field>"; nested messages use
// dotted paths, a map entry "<field>[<key>]". Body-like fields (bodyLike)
// are left out, as REST's digest leaves out the body. nil for anything
// that is not a message.
func protoParams(req any) map[string][]string {
	msg, ok := req.(proto.Message)
	if !ok || msg == nil {
		return nil
	}
	params := map[string][]string{}
	flattenMessage("req", msg.ProtoReflect(), params)
	return params
}

func flattenMessage(prefix string, m protoreflect.Message, out map[string][]string) {
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		name := prefix + "." + string(fd.Name())
		switch {
		case bodyLike(fd, v):
		case fd.IsMap():
			v.Map().Range(func(k protoreflect.MapKey, mv protoreflect.Value) bool {
				flattenValue(fmt.Sprintf("%s[%s]", name, k.String()), fd.MapValue(), mv, out)
				return true
			})
		case fd.IsList():
			list := v.List()
			for i := 0; i < list.Len(); i++ {
				flattenValue(name, fd, list.Get(i), out)
			}
		default:
			flattenValue(name, fd, v, out)
		}
		return true
	})
}

func flattenValue(name string, fd protoreflect.FieldDescriptor, v protoreflect.Value, out map[string][]string) {
	if fd.Message() != nil && !fd.IsMap() {
		flattenMessage(name, v.Message(), out)
		return
	}
	if fd.Kind() == protoreflect.StringKind && len(v.String()) > maxDigestString {
		return // a list or map element over the bound
	}
	out[name] = append(out[name], fmt.Sprint(v.Interface()))
}
