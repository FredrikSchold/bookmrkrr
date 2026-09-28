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

// netAllowed lists the only packages permitted to import a networking package.
// This is what makes "bkmr does not phone home" a checkable property rather
// than a claim in the README.
var netAllowed = map[string]bool{
	"github.com/FredrikSchold/bookmrkrr/internal/fetch":           true,
	"github.com/FredrikSchold/bookmrkrr/internal/capture/browser": true,
}

// netDenied are the import paths that mean "this package can open a
// connection". The README claims the tool's entire network surface is two
// files, so the rule has to cover every way of opening a socket and not just
// the one the two allowed packages happen to use: a new package could import
// net and call net.Dial, or reach for net/rpc, and a check for the literal
// string "net/http" would stay green while the claim quietly became false.
//
// crypto/tls is here rather than with the cryptography rule because importing
// it means dialing, not encrypting a file.
var netDenied = map[string]bool{
	"net":               true,
	"net/http":          true,
	"net/http/httputil": true,
	"net/rpc":           true,
	"net/smtp":          true,
	"crypto/tls":        true,
}

// netDeniedPrefixes cover whole trees. golang.org/x/net/ is all networking, so
// every package under it is denied without listing them.
var netDeniedPrefixes = []string{"golang.org/x/net/"}

// networkImport reports whether an import path lets a package talk to a host.
//
// "net" is matched exactly and never as a prefix. net/url is pure string
// parsing, internal/model uses it on every URL it normalizes, and net/netip is
// address parsing with no dialing in it - a prefix test would fail the build on
// all of them. That kind of false positive is how a guard gets deleted instead
// of fixed, so TestTheNetworkDenylistDoesNotCatchNetURL pins it.
//
// os/exec is deliberately NOT here. internal/tui/browser.go launches the user's
// browser with it and atotto/clipboard shells out to pbpaste and friends; both
// are legitimate and neither is bkmr sending anything anywhere. So the rule
// means: no package outside internal/fetch and internal/capture/browser may open
// a socket from inside this process. It does not, and cannot, bound what a
// subprocess does - a change that shells out to curl would pass every test here
// and has to be caught in review.
func networkImport(imp string) bool {
	if netDenied[imp] {
		return true
	}
	for _, prefix := range netDeniedPrefixes {
		if strings.HasPrefix(imp, prefix) {
			return true
		}
	}
	return false
}

func TestOnlyApprovedPackagesReachTheNetwork(t *testing.T) {
	for pkg, imports := range nonTestImports(t) {
		if netAllowed[pkg] {
			continue
		}
		for _, imp := range imports {
			if networkImport(imp) {
				t.Errorf("%s imports %s, which is only allowed in internal/fetch and internal/capture/browser.\n"+
					"If this package genuinely needs the network, that is a design decision - discuss it before adding the import.", pkg, imp)
			}
		}
	}
}

// TestTheNetworkDenylistCoversEveryWayToOpenAConnection is the positive half:
// the paths a package would reach for if it wanted to talk to a host must all be
// denied, including one under a prefix, so the prefix branch is covered even
// though nothing in this module imports golang.org/x/net.
func TestTheNetworkDenylistCoversEveryWayToOpenAConnection(t *testing.T) {
	for _, path := range []string{
		"net", "net/http", "net/http/httputil", "net/rpc", "net/smtp", "crypto/tls",
		"golang.org/x/net/proxy", "golang.org/x/net/html/charset",
	} {
		if !networkImport(path) {
			t.Errorf("networkImport(%q) = false; that import can reach a host and must be denied "+
				"outside internal/fetch and internal/capture/browser", path)
		}
	}
}

// TestTheNetworkDenylistDoesNotCatchNetURL is the false-positive half of the
// rule. net/url is imported by internal/model, which is not allowlisted and
// must never be, so if the matching above ever became a prefix test the whole
// build would fail on a package doing nothing but parsing strings.
func TestTheNetworkDenylistDoesNotCatchNetURL(t *testing.T) {
	for _, harmless := range []string{"net/url", "net/netip", "net/mail"} {
		if networkImport(harmless) {
			t.Errorf("networkImport(%q) = true; that package cannot open a connection, and denying it "+
				"would fail the build on code that only parses strings. Match \"net\" exactly, never as a prefix.", harmless)
		}
	}
	if !networkImport("net") {
		t.Error(`networkImport("net") = false; net.Dial is the plainest way to open a connection and must be denied`)
	}

	// Not hypothetical: assert net/url really is imported outside the allowlist,
	// so this test is guarding a live case rather than a retired one.
	const model = "github.com/FredrikSchold/bookmrkrr/internal/model"
	imports := nonTestImports(t)
	if netAllowed[model] {
		t.Fatalf("%s is in netAllowed; pick another non-allowlisted package that imports net/url", model)
	}
	if !contains(imports[model], "net/url") {
		t.Errorf("%s no longer imports net/url; find another non-allowlisted importer of it and name that "+
			"here, or this test stops proving the denylist tolerates net/url in real code", model)
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
//
// If you are here because this failed while refactoring internal/fetch: nothing
// is wrong with your code. The rule above can only prove something when it has
// something to look at, and this test is what notices that it stopped. Move the
// network code to its new home, update netAllowed, and this passes again.
func TestTheNetworkAllowlistIsNotIgnoringTestFiles(t *testing.T) {
	imports := nonTestImports(t)
	for pkg := range netAllowed {
		if !contains(imports[pkg], "net/http") {
			t.Errorf("%s does not import net/http in its non-test files; either it was refactored "+
				"(drop it from netAllowed) or nonTestImports is reading nothing, which would make "+
				"TestOnlyApprovedPackagesReachTheNetwork pass vacuously", pkg)
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
