// Package boundary holds architectural tests: rules about the shape of the
// codebase that CI must enforce, not behavior of any one package.
//
// These tests exist so that the promises in README.md are checkable facts
// rather than prose. Do not edit a rule here to make a change pass - see
// CONTRIBUTING.md.
package boundary

import (
	"strings"
	"testing"
)

// netAllowed lists the only packages permitted to import net/http. This is
// what makes "bkmr does not phone home" a checkable property rather than a
// claim in the README.
var netAllowed = map[string]bool{
	"github.com/FredrikSchold/bookmrkrr/internal/fetch":           true,
	"github.com/FredrikSchold/bookmrkrr/internal/capture/browser": true,
}

func TestOnlyApprovedPackagesImportNetHTTP(t *testing.T) {
	for pkg, imports := range nonTestImports(t) {
		for _, imp := range imports {
			if imp != "net/http" {
				continue
			}
			if !netAllowed[pkg] {
				t.Errorf("%s imports net/http, which is only allowed in internal/fetch and internal/capture/browser.\n"+
					"If this package genuinely needs the network, that is a design decision - discuss it before adding the import.", pkg)
			}
		}
	}
}

// TestTheNetworkAllowlistIsNotIgnoringTestFiles guards the guard above.
//
// nonTestImports relies on go list's Imports field excluding _test.go files,
// which is why cmd/bkmr/fetch_test.go may import net/http without tripping the
// rule. If that assumption were wrong in the other direction - if Imports were
// empty, or the allowlisted packages stopped appearing - the network rule would
// pass vacuously and nobody would notice. So assert the positive case too: the
// two packages that are supposed to reach the network must actually be seen to
// import net/http.
func TestTheNetworkAllowlistIsNotIgnoringTestFiles(t *testing.T) {
	imports := nonTestImports(t)
	for pkg := range netAllowed {
		if !contains(imports[pkg], "net/http") {
			t.Errorf("%s does not import net/http in its non-test files; either it was refactored "+
				"(drop it from netAllowed) or nonTestImports is reading nothing, which would make "+
				"TestOnlyApprovedPackagesImportNetHTTP pass vacuously", pkg)
		}
	}
	// cmd/bkmr genuinely imports net/http from fetch_test.go and must not be
	// reported for it. Stated as a test so a future change to how imports are
	// collected cannot quietly start counting test files.
	const cli = "github.com/FredrikSchold/bookmrkrr/cmd/bkmr"
	if _, ok := imports[cli]; !ok {
		t.Fatalf("%s is missing from go list output", cli)
	}
	if contains(imports[cli], "net/http") {
		t.Errorf("%s appears to import net/http outside its tests; the CLI must reach the network "+
			"only through internal/fetch", cli)
	}
}

// cryptoAllowed lists the only packages permitted to import cryptographic
// primitives. All crypto lives in internal/crypto (spec section 3).
var cryptoAllowed = map[string]bool{
	"github.com/FredrikSchold/bookmrkrr/internal/crypto": true,
}

// cryptoPrefixes are the import paths that mean "this package is doing
// cryptography". golang.org/x/term is a terminal helper and does not match.
var cryptoPrefixes = []string{"golang.org/x/crypto/", "crypto/aes", "crypto/cipher", "crypto/sha", "crypto/hmac"}

func TestOnlyTheCryptoPackageImportsCryptoPrimitives(t *testing.T) {
	for pkg, imports := range nonTestImports(t) {
		if cryptoAllowed[pkg] {
			continue
		}
		for _, imp := range imports {
			for _, prefix := range cryptoPrefixes {
				if strings.HasPrefix(imp, prefix) {
					t.Errorf("%s imports %s; all cryptography belongs in internal/crypto", pkg, imp)
				}
			}
		}
	}
}

// TestDirectDependencyBudget keeps the dependency tree small, which spec
// section 12 treats as a privacy feature in itself: every direct module is code
// that runs with the vault key in the same address space.
func TestDirectDependencyBudget(t *testing.T) {
	const budget = 9

	requires := requireBlock(t)
	if len(requires) > budget {
		t.Errorf("go.mod requires %d direct modules, budget is %d:\n%s\n"+
			"A new direct dependency needs a justification in the PR - a small dependency tree is itself a privacy feature.",
			len(requires), budget, strings.Join(requires, "\n"))
	}
	// A count that reads too low is the more likely failure: `go get` has twice
	// left a module in this project marked // indirect that the build in fact
	// imports directly, which would understate the tree and let a tenth
	// dependency in unnoticed. requireBlock therefore parses the require blocks
	// rather than counting lines, and this floor fails if it ever silently
	// stops finding them.
	if len(requires) < budget {
		t.Errorf("go.mod requires only %d direct modules, expected %d:\n%s\n"+
			"If a dependency was genuinely removed, lower the budget in this test and say so in the PR. "+
			"If it was not, requireBlock has stopped parsing go.mod correctly and the budget above is meaningless.",
			len(requires), budget, strings.Join(requires, "\n"))
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
