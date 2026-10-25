package federation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/accessmarker"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// A local push writes into a protected or public target only through
// its daemon: the direct write (and the watermark read) is refused and
// the target is left untouched. A private-marked target is pushed as an
// unmarked one.
func TestLocalPusher_AccessClass(t *testing.T) {
	for _, access := range []string{config.AccessPrivate, config.AccessProtected, config.AccessPublic} {
		t.Run(access, func(t *testing.T) {
			targetDrv, targetPath := newTestDB(t)
			if err := accessmarker.Write(targetPath, accessmarker.Marker{Access: access, Instance: "peer"}); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			pusher := NewLocalPusher("fed", targetPath, nil, storageutil.NewTestBlobStore(t))
			err := pusher.Push(ctx, []storage.KnowledgeObject{makeObject("obj-1", "hash-1")}, nil, nil)
			_, wmErr := pusher.Watermark(ctx)

			if access == config.AccessPrivate {
				if err != nil || wmErr != nil {
					t.Fatalf("push into a private target = %v, watermark %v", err, wmErr)
				}
				if n := countObjects(t, targetDrv); n != 1 {
					t.Errorf("private target holds %d objects; want 1", n)
				}
				return
			}
			for name, e := range map[string]error{"push": err, "watermark": wmErr} {
				if !errors.Is(e, ErrLocalTargetNotPrivate) || !errors.Is(e, accessmarker.ErrRefused) {
					t.Errorf("%s into a %s target = %v; want the posture refusal", name, access, e)
				}
			}
			if n := countObjects(t, targetDrv); n != 0 {
				t.Errorf("refused push wrote %d objects into the %s target", n, access)
			}
		})
	}
}

// A path entry naming a protected target is a configuration error: the
// worker set refuses it at startup, naming the entry; a URL entry or a
// private target passes.
func TestNew_RefusesNonPrivateLocalTarget(t *testing.T) {
	src, _ := newTestDB(t)
	_, protectedPath := newTestDB(t)
	_, privatePath := newTestDB(t)
	if err := accessmarker.Write(protectedPath, accessmarker.Marker{Access: config.AccessProtected}); err != nil {
		t.Fatal(err)
	}
	ok := config.Config{Federations: []config.FederationEntry{
		{Name: "lan", URL: "http://127.0.0.1:1", SyncMode: "async"},
		{Name: "near", URL: privatePath, SyncMode: "async"},
	}}
	if _, err := New(ok, src); err != nil {
		t.Fatalf("New with a URL and a private path entry: %v", err)
	}
	bad := config.Config{Federations: []config.FederationEntry{{Name: "vault", URL: "file://" + protectedPath, SyncMode: "async"}}}
	_, err := New(bad, src)
	if !errors.Is(err, ErrLocalTargetNotPrivate) {
		t.Fatalf("New with a protected path entry = %v; want ErrLocalTargetNotPrivate", err)
	}
	if !strings.Contains(err.Error(), `"vault"`) || !strings.Contains(err.Error(), "URL") {
		t.Errorf("error %q does not name the entry and the URL fix", err)
	}
}
