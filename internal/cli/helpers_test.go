package cli

import (
	"bytes"
	"testing"
)

// testDeps is replaced by a fuller fixture in Task 9.
func testDeps(t *testing.T) (Deps, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errb bytes.Buffer
	return Deps{Env: map[string]string{}, Stdout: &out, Stderr: &errb}, &out, &errb
}
