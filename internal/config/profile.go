package config

// Active resolves the focus profile a command runs under, the one rule
// both binaries share:
//
//  1. --profile when passed (flagSet), even empty: an explicit "" selects
//     no profile;
//  2. profile.default — Load has already merged CTXT_PROFILE over the
//     config file into it;
//  3. none: "" leaves the choice to the server's own default.
func (p ProfileConfig) Active(flag string, flagSet bool) string {
	if flagSet {
		return flag
	}
	return p.Default
}
