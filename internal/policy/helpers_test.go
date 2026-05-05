package policy_test

import "os"

// writeFileImpl wraps os.WriteFile so tests can call writeFile without
// importing os directly. Keeps the body of policy_test.go focused on
// the policy assertions instead of stdlib boilerplate.
func writeFileImpl(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
