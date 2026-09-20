package integration

// T-0191: Agent API returns graph and projection results.
// Surfaces: REST (GET /objects/{id}, GET /search), gRPC (GetObject, NodeAwareSearch).

import (
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	grpcserver "github.com/ideacrafterslabs/ctxt/internal/server/grpc"
	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// ─── fixture ─────────────────────────────────────────────────────────────────

// seedGraphKO builds a graph-canonical KO and stores it via the driver.
func seedGraphKO(t *testing.T, env *testEnv, id, typ, content string, tags ...string) {
	t.Helper()
	ko := storageutil.BuildGraphKO(id, typ, content, tags...)
	err := env.svc.Store.Objects().Create(context.Background(), ko)
	require.NoError(t, err, "seedGraphKO %s", id)
}

// ─── gRPC test server ─────────────────────────────────────────────────────────

// startGRPCTestServer spins up a gRPC server backed by the testEnv's service
// and returns a connected client connection. The server is stopped via t.Cleanup.
func startGRPCTestServer(t *testing.T, svc *service.Service) *grpc.ClientConn {
	t.Helper()

	// Pick a free port by binding + immediately releasing.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	ln.Close()

	srv := grpcserver.New(addr, svc)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() {
		if err := srv.Start(ctx); err != nil && ctx.Err() == nil {
			t.Logf("grpc server stopped: %v", err)
		}
	}()

	// Wait for the server to be ready (up to 500 ms).
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if conn, err := net.Dial("tcp", addr); err == nil {
			conn.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err, "grpc dial %s", addr)
	t.Cleanup(func() { conn.Close() })
	return conn
}

// ─── REST: GET /objects/{id} ──────────────────────────────────────────────────

// TestT0191_REST_GetObject_GraphRoundTrip verifies graph nodes survive the
// ingest→store→retrieve cycle via GET /objects/{id}.
func TestT0191_REST_GetObject_GraphRoundTrip(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	const (
		objID   = "t0191-rest-get"
		objType = "article"
		content = "graph round-trip content"
	)
	seedGraphKO(t, env, objID, objType, content, "alpha", "beta")

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, objID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode, "unexpected HTTP status")

	var obj pluginapi.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, objID, obj.ID)
	require.NotNil(t, obj.Graph, "Graph must be non-nil after round-trip")
	require.NotEmpty(t, obj.Graph.Nodes, "Graph.Nodes must be non-empty")

	// summary + section + 2 tag nodes = 4 minimum.
	assert.GreaterOrEqual(t, len(obj.Graph.Nodes), 4)
	nodeTypes := nodeTypeSet(obj.Graph.Nodes)
	assert.True(t, nodeTypes[pluginapi.NodeTypeSummary], "summary node missing")
	assert.True(t, nodeTypes[pluginapi.NodeTypeSection], "section node missing")
	assert.True(t, nodeTypes[pluginapi.NodeTypeTag], "tag node missing")
}

// TestT0191_REST_GetObject_NodeIDsStable verifies that each node ID in the
// retrieved object can be parsed via ParseNodeID and points back to the object.
func TestT0191_REST_GetObject_NodeIDsStable(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	const objID = "t0191-nodeid"
	seedGraphKO(t, env, objID, "note", "stable id content")

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, objID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var obj pluginapi.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))
	require.NotNil(t, obj.Graph)

	for _, n := range obj.Graph.Nodes {
		ref, err := pluginapi.ParseNodeID(n.ID)
		assert.NoError(t, err, "ParseNodeID(%q)", n.ID)
		assert.Equal(t, objID, ref.ObjectID, "node ObjectID mismatch in %q", n.ID)
		assert.NotEmpty(t, ref.NodeType, "node NodeType empty in %q", n.ID)
		assert.GreaterOrEqual(t, ref.Ordinal, 0, "negative ordinal in %q", n.ID)
	}
}

// TestT0191_REST_GetObject_ProjectionConsistent verifies that flat projected
// fields (Sections, Tags, Summaries) returned on the KO are consistent with
// what ProjectDocument / ProjectIndex derive from the stored Graph.
func TestT0191_REST_GetObject_ProjectionConsistent(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	const (
		objID   = "t0191-proj"
		content = "projection consistency check"
	)
	seedGraphKO(t, env, objID, "text", content, "tag-x")

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, objID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var obj pluginapi.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	tagLabels := make([]string, len(obj.Tags))
	for i, tg := range obj.Tags {
		tagLabels[i] = tg.Label
	}
	assert.Contains(t, tagLabels, "tag-x", "flat Tags must reflect graph tag node")

	require.NotEmpty(t, obj.Sections, "Sections must be non-empty")
	assert.Equal(t, content, obj.Sections[0].Content, "section content mismatch")

	require.NotEmpty(t, obj.Summaries, "Summaries must be non-empty")
	assert.Equal(t, content, obj.Summaries[0], "summary content mismatch")
}

// ─── REST: GET /search ────────────────────────────────────────────────────────

// TestT0191_REST_Search_GraphPreservedInResults verifies that search results
// include Graph nodes (not stripped to flat fields).
func TestT0191_REST_Search_GraphPreservedInResults(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	seedGraphKO(t, env, "t0191-search-1", "article", "graph search content alpha")
	seedGraphKO(t, env, "t0191-search-2", "note", "graph search content beta")

	resp, err := gohttp.Get(env.URL + "/api/v1/search?q=type==article")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var body struct {
		Data  []pluginapi.KnowledgeObject `json:"data"`
		Total int                         `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, 1, body.Total)
	require.Len(t, body.Data, 1)

	obj := body.Data[0]
	assert.Equal(t, "t0191-search-1", obj.ID)
	require.NotNil(t, obj.Graph, "Graph must survive search round-trip")
	assert.NotEmpty(t, obj.Graph.Nodes)
}

// TestT0191_REST_Search_AllResultsHaveGraphs ensures every object in a
// multi-result search response carries its graph.
func TestT0191_REST_Search_AllResultsHaveGraphs(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("t0191-multi-%d", i)
		seedGraphKO(t, env, id, "memo", fmt.Sprintf("multi-graph content %d", i))
	}

	resp, err := gohttp.Get(env.URL + "/api/v1/search?q=type==memo")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var body struct {
		Data  []pluginapi.KnowledgeObject `json:"data"`
		Total int                         `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, 3, body.Total)

	for i, obj := range body.Data {
		require.NotNil(t, obj.Graph, "result[%d] Graph nil", i)
		assert.NotEmpty(t, obj.Graph.Nodes, "result[%d] Graph.Nodes empty", i)
	}
}

// ─── gRPC: GetObject ─────────────────────────────────────────────────────────

// TestT0191_GRPC_GetObject_GraphPresent verifies GetObject returns a proto
// KnowledgeObject with graph nodes matching the stored graph.
func TestT0191_GRPC_GetObject_GraphPresent(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	const (
		objID   = "t0191-grpc-get"
		content = "grpc graph content"
	)
	seedGraphKO(t, env, objID, "article", content, "grpc-tag")

	conn := startGRPCTestServer(t, env.svc)
	c := pb.NewQueryServiceClient(conn)

	protoObj, err := c.GetObject(context.Background(), &pb.GetObjectRequest{Id: objID})
	require.NoError(t, err, "GetObject RPC failed")

	assert.Equal(t, objID, protoObj.Id)
	require.NotNil(t, protoObj.Graph, "proto Graph must be non-nil")
	require.NotEmpty(t, protoObj.Graph.Nodes)

	protoNodeTypes := protoNodeTypeSet(protoObj.Graph.Nodes)
	assert.True(t, protoNodeTypes[pluginapi.NodeTypeSummary], "summary node missing")
	assert.True(t, protoNodeTypes[pluginapi.NodeTypeSection], "section node missing")
	assert.True(t, protoNodeTypes[pluginapi.NodeTypeTag], "tag node missing")
}

// TestT0191_GRPC_GetObject_NodeIDsStable verifies proto node IDs are
// parseable via ParseNodeID and reference the correct object.
func TestT0191_GRPC_GetObject_NodeIDsStable(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	const objID = "t0191-grpc-nodeid"
	seedGraphKO(t, env, objID, "note", "stable node id via grpc")

	conn := startGRPCTestServer(t, env.svc)
	c := pb.NewQueryServiceClient(conn)

	protoObj, err := c.GetObject(context.Background(), &pb.GetObjectRequest{Id: objID})
	require.NoError(t, err)
	require.NotNil(t, protoObj.Graph)

	for _, n := range protoObj.Graph.Nodes {
		ref, err := pluginapi.ParseNodeID(n.Id)
		assert.NoError(t, err, "ParseNodeID(%q)", n.Id)
		assert.Equal(t, objID, ref.ObjectID)
		assert.GreaterOrEqual(t, ref.Ordinal, 0)
	}
}

// ─── gRPC: NodeAwareSearch ───────────────────────────────────────────────────

// TestT0191_GRPC_NodeAwareSearch_DocumentView verifies NodeAwareSearch results
// include a non-nil DocumentView derived via ProjectDocument.
func TestT0191_GRPC_NodeAwareSearch_DocumentView(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	seedGraphKO(t, env, "t0191-nas-1", "post", "node aware search content alpha")
	seedGraphKO(t, env, "t0191-nas-2", "draft", "node aware search content beta")

	conn := startGRPCTestServer(t, env.svc)
	c := pb.NewQueryServiceClient(conn)

	resp, err := c.NodeAwareSearch(context.Background(), &pb.NodeAwareSearchRequest{
		Query: "type==post",
		Limit: 10,
	})
	require.NoError(t, err, "NodeAwareSearch RPC failed")
	require.Equal(t, int32(1), resp.Total)
	require.Len(t, resp.Results, 1)

	result := resp.Results[0]
	require.NotNil(t, result.Object)
	assert.Equal(t, "t0191-nas-1", result.Object.Id)

	require.NotNil(t, result.DocumentView, "DocumentView must be non-nil")
	assert.NotEmpty(t, result.DocumentView.Sections, "DocumentView.Sections must be non-empty")
}

// TestT0191_GRPC_NodeAwareSearch_NodeHits verifies that ReturnNodeHits=true
// produces NodeHit entries with valid NodeURI refs for each node.
func TestT0191_GRPC_NodeAwareSearch_NodeHits(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	const objID = "t0191-nhits"
	seedGraphKO(t, env, objID, "insight", "node hit verification content")

	conn := startGRPCTestServer(t, env.svc)
	c := pb.NewQueryServiceClient(conn)

	resp, err := c.NodeAwareSearch(context.Background(), &pb.NodeAwareSearchRequest{
		Query: "type==insight",
		Limit: 10,
		Filter: &pb.NodeAwareFilter{
			ReturnNodeHits: true,
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)

	result := resp.Results[0]
	require.NotEmpty(t, result.NodeHits, "NodeHits must be non-empty when ReturnNodeHits=true")

	for _, nh := range result.NodeHits {
		assert.Equal(t, objID, nh.ObjectId, "NodeHit.ObjectId")
		assert.NotEmpty(t, nh.NodeRef, "NodeHit.NodeRef")
		assert.NotEmpty(t, nh.NodeType, "NodeHit.NodeType")
		assert.Greater(t, nh.Score, float64(0), "NodeHit.Score > 0")

		ref, err := pluginapi.ParseNodeURI(nh.NodeRef)
		assert.NoError(t, err, "ParseNodeURI(%q)", nh.NodeRef)
		assert.Equal(t, objID, ref.ObjectID, "URI ObjectID")
	}
}

// TestT0191_GRPC_NodeAwareSearch_NodeTypeFilter verifies that NodeTypes filter
// restricts results to objects containing the requested node type.
func TestT0191_GRPC_NodeAwareSearch_NodeTypeFilter(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	// Same type; only one has a tag node.
	seedGraphKO(t, env, "t0191-ntf-tagged", "spec", "tagged spec content", "my-tag")
	seedGraphKO(t, env, "t0191-ntf-bare", "spec", "bare spec content")

	conn := startGRPCTestServer(t, env.svc)
	c := pb.NewQueryServiceClient(conn)

	resp, err := c.NodeAwareSearch(context.Background(), &pb.NodeAwareSearchRequest{
		Query: "type==spec",
		Limit: 10,
		Filter: &pb.NodeAwareFilter{
			NodeTypes: []string{pluginapi.NodeTypeTag},
		},
	})
	require.NoError(t, err)

	// Only the tagged object should match the filter.
	require.Equal(t, int32(1), resp.Total, "only tag-bearing object expected")
	require.Len(t, resp.Results, 1)
	assert.Equal(t, "t0191-ntf-tagged", resp.Results[0].Object.Id)
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func nodeTypeSet(nodes []pluginapi.GraphNode) map[string]bool {
	m := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		m[n.NodeType] = true
	}
	return m
}

func protoNodeTypeSet(nodes []*pb.GraphNode) map[string]bool {
	m := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		m[n.NodeType] = true
	}
	return m
}
