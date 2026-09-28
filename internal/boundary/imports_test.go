package boundary

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// nonTestImports maps each package in this module to its non-test imports.
//
// go list's Imports field holds only the imports of a package's non-test files:
// the imports that appear solely in _test.go files land in TestImports, and
// those of an external _test package in XTestImports. That is what lets
// cmd/bkmr/fetch_test.go import net/http - it exercises internal/fetch against
// an httptest server - without the CLI tripping the network rule. Decoding only
// Imports is therefore deliberate, and TestTheNetworkAllowlistIsNotIgnoringTestFiles
// asserts both halves of that behavior so the rule cannot pass vacuously.
func nonTestImports(t *testing.T) map[string][]string {
	t.Helper()
	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = moduleRoot(t)
	data, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -json ./...: %v", err)
	}

	result := map[string][]string{}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	for dec.More() {
		var p struct {
			ImportPath string
			Imports    []string
		}
		if err := dec.Decode(&p); err != nil {
			t.Fatalf("decode go list output: %v", err)
		}
		result[p.ImportPath] = p.Imports
	}
	if len(result) == 0 {
		t.Fatal("go list returned no packages")
	}
	return result
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	outBytes, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	gomod := strings.TrimSpace(string(outBytes))
	if gomod == "" || gomod == os.DevNull {
		t.Fatal("not inside a Go module")
	}
	return filepath.Dir(gomod)
}

// requireBlock returns the module paths in go.mod's direct require block.
//
// Parsed rather than counted: `go get` has twice left a module in this project
// marked // indirect, so a line count would understate the direct tree. Lines
// carrying that marker are skipped, and both the block and single-line forms of
// require are handled.
func requireBlock(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(moduleRoot(t), "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}

	var direct []string
	inBlock := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "require ("):
			inBlock = true
		case inBlock && trimmed == ")":
			inBlock = false
		case inBlock && trimmed != "" && !strings.HasPrefix(trimmed, "//"):
			if strings.Contains(trimmed, "// indirect") {
				continue
			}
			direct = append(direct, strings.Fields(trimmed)[0])
		case !inBlock && strings.HasPrefix(trimmed, "require ") && !strings.Contains(trimmed, "// indirect"):
			direct = append(direct, strings.Fields(trimmed)[1])
		}
	}
	return direct
}
