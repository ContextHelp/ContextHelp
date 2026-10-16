package service

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/schemaevolution"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// SchemaEvolutionSuggestion is a single suggested addition.
type SchemaEvolutionSuggestion = schemaevolution.Suggestion

// SchemaEvolutionResult holds all suggestions for a profile schema.
type SchemaEvolutionResult = schemaevolution.Result

// SuggestSchemaEvolution scans the most recent objects and suggests
// entity types and topics not yet in the profile schema
// (schemaevolution.Suggest). Basic frequency analysis, no LLM.
func (s *Service) SuggestSchemaEvolution(
	ctx context.Context,
	profileName string,
	schema config.ProfileSchema,
) (*SchemaEvolutionResult, error) {
	objects, _, err := s.Store(ctx).Objects().List(ctx, storage.ObjectFilter{
		Limit: schemaevolution.ScanLimit,
		Sort:  "created_at",
		Dir:   "desc",
	})
	if err != nil {
		return nil, err
	}
	return schemaevolution.Suggest(profileName, schema, objects), nil
}
