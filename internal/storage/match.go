package storage

import "context"

// SQLPredicate is a raw predicate over one objects row's columns,
// unqualified, in the driver's dialect, with ? placeholders whatever
// the dialect and its values bound in Args: the form Scope.SQL takes.
// A driver that numbers placeholders renumbers it into its statement.
type SQLPredicate struct {
	SQL  string
	Args []any
}

func (v restrictedObjects) MatchBySQL(ctx context.Context, ids []string, preds []SQLPredicate) (map[string][]bool, error) {
	if v.scoped == nil {
		return nil, refuseRead("Objects", "MatchBySQL", v.refuse)
	}
	return v.scoped.MatchBySQL(ctx, ids, preds)
}
