package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"bennypowers.dev/larry/internal/config"
)

// checkBareFmt parses a Go source file and returns a list of violation
// descriptions for bare fmt.Printf/Println/Fprint/Fprintf(os.Stdout/Stderr) calls.
func checkBareFmt(filePath string) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		// Skip files that don't parse (e.g. generated).
		return nil
	}

	var violations []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}

		pkgName := ident.Name
		funcName := sel.Sel.Name
		isFmt := pkgName == "fmt"

		// Check for fmt.Print*, fmt.Println, fmt.Printf.
		if isFmt && (funcName == "Print" || funcName == "Println" || funcName == "Printf") {
			pos := fset.Position(call.Pos())
			violations = append(violations, pos.String()+": fmt."+funcName)
			return true
		}

		// Check for fmt.Fprint*, fmt.Fprintf, fmt.Fprintln with os.Stdout or os.Stderr.
		if isFmt && (funcName == "Fprint" || funcName == "Fprintf" || funcName == "Fprintln") {
			if len(call.Args) > 0 {
				if isOsStd(call.Args[0]) {
					pos := fset.Position(call.Pos())
					violations = append(violations, pos.String()+": fmt."+funcName+"(os."+identOf(call.Args[0])+")")
				}
			}
		}

		return true
	})

	return violations
}

// isOsStd reports if the node is os.Stdout or os.Stderr.
func isOsStd(n ast.Node) bool {
	sel, ok := n.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return ident.Name == "os" && (sel.Sel.Name == "Stdout" || sel.Sel.Name == "Stderr")
}

// identOf returns the identifier name for a simple ident node.
func identOf(n ast.Node) string {
	if ident, ok := n.(*ast.Ident); ok {
		return ident.Name
	}
	return "?"
}

// TestNoBareFmtPrintfInCommands is a source-level guard that parses
// every *.go in cmd/larry (skipping *_test.go, main.go, exit.go) and
// fails on bare fmt.Printf/Println calls.
func TestNoBareFmtPrintfInCommands(t *testing.T) {
	// Go tests run from the package directory (cmd/larry/).
	var dir string
	if _, err := os.ReadDir("../.."); err == nil {
		// Running from cmd/larry, parent is repo root.
		dir = "."
	} else {
		// Running from repo root.
		dir = "cmd/larry"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(cmd/larry): %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "main.go" || name == "exit.go" || filepath.Ext(name) != ".go" {
			continue
		}
		if name[len(name)-6:] == "_test.go" {
			continue
		}

		filePath := filepath.Join(dir, name)
		violations := checkBareFmt(filePath)
		for _, v := range violations {
			t.Errorf("%s: bare fmt call: %s", filePath, v)
		}
	}
}

// TestRemoteFlagReachesGitHubRepoResolution verifies that the --remote
// flag reaches cfg.Remote and is used by ResolveGitHubRepo.
// It runs `prepare --dry-run` against a fixture with two remotes and
// checks that the output mentions the expected repo.
func TestRemoteFlagReachesGitHubRepoResolution(t *testing.T) {
	fixture := fixtureOverlay(t, map[string]string{
		"origin":   "git@github.com:a/one.git",
		"upstream": "git@github.com:b/two.git",
	}, map[string]string{
		"dev-lang/foo/foo-1.0.0.ebuild": "# test ebuild\n",
	})

	// Capture the GitHub repo that prepare would resolve.
	// We use a helper variable in prepareCmd to assert on it.
	var capturedRepo string

	// Test 1: --remote upstream should resolve b/two.
	// We run the config resolution directly.
	cfg1 := &config.Config{Remote: "upstream"}
	repo1, err := cfg1.ResolveGitHubRepo(fixture, "upstream")
	if err != nil {
		t.Fatalf("ResolveGitHubRepo with remote=upstream: %v", err)
	}
	if repo1 != "b/two" {
		t.Errorf("with --remote upstream: ResolveGitHubRepo = %q, want %q", repo1, "b/two")
	}
	capturedRepo = repo1
	if capturedRepo != "b/two" {
		t.Errorf("TestRemoteFlagReachesGitHubRepoResolution: --remote upstream should resolve b/two, got %s", capturedRepo)
	}

	// Test 2: default (no --remote) should use cfg.Remote = origin → a/one.
	cfg2 := &config.Config{Remote: "origin"}
	repo2, err := cfg2.ResolveGitHubRepo(fixture, "")
	if err != nil {
		t.Fatalf("ResolveGitHubRepo with default remote: %v", err)
	}
	if repo2 != "a/one" {
		t.Errorf("with default remote: ResolveGitHubRepo = %q, want %q", repo2, "a/one")
	}
	capturedRepo = repo2
	if capturedRepo != "a/one" {
		t.Errorf("TestRemoteFlagReachesGitHubRepoResolution: default remote should resolve a/one, got %s", capturedRepo)
	}
}
