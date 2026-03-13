package aliasing

// resolver.go documents the alias resolution strategy.
//
// Resolution order (for a given alias + profile):
//  1. Profile-scoped alias (scope="profile", profile=<activeProfile>)
//  2. Global alias (scope="global")
//  3. Input returned unchanged (treated as a direct object ID)
//
// This means a profile-scoped alias shadows a global alias of the same name.
// The service layer calls ResolveAlias before every object lookup, making
// alias resolution fully transparent to callers.
