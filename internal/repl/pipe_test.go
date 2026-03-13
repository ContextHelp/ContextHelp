package repl

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
)

func TestParsePipe(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantLHS  string
		wantRHS  string
		wantPipe bool
	}{
		{
			name:     "simple pipe",
			input:    "find auth | make brief",
			wantLHS:  "find auth",
			wantRHS:  "make brief",
			wantPipe: true,
		},
		{
			name:     "query without command token",
			input:    "authentication flow | make summary",
			wantLHS:  "authentication flow",
			wantRHS:  "make summary",
			wantPipe: true,
		},
		{
			name:     "no pipe",
			input:    "find authentication",
			wantPipe: false,
		},
		{
			name:     "RSQL value with pipe char no spaces",
			input:    "type==url|pdf",
			wantPipe: false,
		},
		{
			name:     "quoted pipe is not a pipe separator",
			input:    `find "pipe | dream" | make brief`,
			wantLHS:  `find "pipe | dream"`,
			wantRHS:  "make brief",
			wantPipe: true,
		},
		{
			name:     "pipe with extra whitespace trimmed",
			input:    "checkout UX   |   make plan",
			wantLHS:  "checkout UX",
			wantRHS:  "make plan",
			wantPipe: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lhs, rhs, isPipe := ParsePipe(tt.input)
			assert.Equal(t, tt.wantPipe, isPipe)
			if tt.wantPipe {
				assert.Equal(t, tt.wantLHS, lhs)
				assert.Equal(t, tt.wantRHS, rhs)
			}
		})
	}
}

func TestBuildPipeCommand(t *testing.T) {
	objs := []*storage.KnowledgeObject{
		{ID: "id-001"},
		{ID: "id-002"},
	}
	got := BuildPipeCommand(objs, "brief")
	assert.Equal(t, `make brief --q "id=in=(id-001,id-002)"`, got)
}

func TestBuildPipeCommand_Single(t *testing.T) {
	objs := []*storage.KnowledgeObject{{ID: "only-one"}}
	got := BuildPipeCommand(objs, "summary")
	assert.Equal(t, `make summary --q "id=in=(only-one)"`, got)
}

func TestBuildPipeCommand_Empty(t *testing.T) {
	got := BuildPipeCommand(nil, "brief")
	assert.Equal(t, "", got)
}
