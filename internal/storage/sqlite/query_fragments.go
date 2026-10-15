package sqlite

// SQL fragments repeated across the list queries. They are fixed text: every
// value they compare against is bound as a ? argument.
const (
	// orderNewestFirst sorts a list query by creation time, newest first.
	orderNewestFirst = " ORDER BY created_at DESC"
	// andProfileScoped narrows a query to one profile's rows.
	andProfileScoped = " AND profile_id = ?"
)

// profilePage completes base, a SELECT ending in "WHERE 1=1", with the
// optional profile scope, the orderBy clause, and the optional limit/offset
// page. It returns the query and its arguments in placeholder order.
func profilePage(base, orderBy, profileID string, limit, offset int) (string, []any) {
	query := base
	var args []any
	if profileID != "" {
		query += andProfileScoped
		args = append(args, profileID)
	}
	query += orderBy
	if limit > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, limit, offset)
	}
	return query, args
}
