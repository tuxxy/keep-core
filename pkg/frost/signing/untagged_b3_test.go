package signing

// Remediation batch 3 acceptance test for tuxxy/keep-core#64 (K09-49).
//
// Invariant: every batch-3 owning test runs in a default, untagged
// `go test` of its package, which is what CI and the legacy-regression stage
// run. A build constraint on an owning test file silently drops its guard.

import (
	"os/exec"
	"strings"
	"testing"
)

// batch3OwningFiles lists, per package, the batch-3 test files that own a
// guard. Add a file here when batch 3 adds one.
var batch3OwningFiles = map[string][]string{
	"github.com/keep-network/keep-core/pkg/frost/signing": {
		"possible_release_b3_test.go",
		"untagged_b3_test.go",
	},
	"github.com/keep-network/keep-core/pkg/frost/movement": {
		"incoming_b3_test.go",
		"incoming_world_b3_test.go",
	},
	"github.com/keep-network/keep-core/pkg/tbtc": {
		"frost_incoming_b3_test.go",
	},
}

func TestBatch3OwningTestsRunUntagged(t *testing.T) {
	for pkg, files := range batch3OwningFiles {
		// `go list` with no -tags reports the test files a default build
		// compiles; files excluded by a build constraint are not listed.
		out, err := exec.Command("go", "list", "-f", "{{range .TestGoFiles}}{{.}}\n{{end}}{{range .XTestGoFiles}}{{.}}\n{{end}}", pkg).CombinedOutput()
		if err != nil {
			t.Fatalf("go list %s: %v\n%s", pkg, err, out)
		}
		compiled := map[string]bool{}
		for _, f := range strings.Fields(string(out)) {
			compiled[f] = true
		}
		for _, f := range files {
			if !compiled[f] {
				t.Errorf("owning test %s/%s does not run in a default untagged go test (build constraint or missing file)", pkg, f)
			}
		}
	}
}
