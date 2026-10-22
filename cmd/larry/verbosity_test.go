package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/atom"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/upstream"
)

func stubUpstreamResults(t *testing.T, results []upstream.UpstreamResult, err error) {
	t.Helper()
	orig := runUpstreamWithTargets
	runUpstreamWithTargets = func(string, *provider.ProviderRegistry, []*atom.Atom, bool, bool) ([]upstream.UpstreamResult, error) {
		return results, err
	}
	t.Cleanup(func() { runUpstreamWithTargets = orig })
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LARRY_VERBOSE", "")
}

func verbosityOverlay(t *testing.T) string {
	t.Helper()
	return fixtureOverlay(t, map[string]string{"origin": "https://example.com/test.git"}, map[string]string{
		"dev-util/foo/foo-1.0.0.ebuild": `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`,
	})
}

func TestUpstreamFilterUsesCfgVerbose(t *testing.T) {
	ov := verbosityOverlay(t)
	stubUpstreamResults(t, []upstream.UpstreamResult{{
		Package: "dev-util/foo",
		Current: "1.0.0",
		Latest:  "1.0.0",
		Status:  upstream.StatusLive,
	}}, nil)

	stdout, stderr, err := execCmd(t, "upstream", "--overlay", ov)
	if err != nil {
		t.Fatalf("default run: %v (stderr %q)", err, stderr)
	}
	if strings.Contains(stdout, "dev-util/foo") {
		t.Errorf("default run showed an insignificant LIVE row: %q", stdout)
	}
	if !strings.Contains(stdout, "All packages are up-to-date") {
		t.Errorf("default run = %q, want the up-to-date message", stdout)
	}

	stdout, stderr, err = execCmd(t, "upstream", "--overlay", ov, "-v")
	if err != nil {
		t.Fatalf("-v run: %v (stderr %q)", err, stderr)
	}
	if !strings.Contains(stdout, "dev-util/foo") || !strings.Contains(stdout, "LIVE") {
		t.Errorf("-v stdout = %q, want the LIVE row", stdout)
	}
}

func TestVerboseConfigFileReachesUpstreamFilter(t *testing.T) {
	ov := verbosityOverlay(t)
	stubUpstreamResults(t, []upstream.UpstreamResult{{
		Package: "dev-util/foo",
		Current: "1.0.0",
		Status:  upstream.StatusLive,
	}}, nil)

	cfgFile := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("verbose: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := execCmd(t, "upstream", "--overlay", ov, "--config", cfgFile)
	if err != nil {
		t.Fatalf("execCmd: %v (stderr %q)", err, stderr)
	}
	if !strings.Contains(stdout, "dev-util/foo") {
		t.Errorf("config verbose:1 did not reveal the LIVE row; stdout = %q", stdout)
	}
}

func TestVerboseFlagReachesLogger(t *testing.T) {
	ov := verbosityOverlay(t)
	_, stderr, err := execCmd(t, "bump", "--dry-run", "--overlay", ov, "-vv", "dev-util/foo")
	if err != nil {
		t.Fatalf("bump -vv: %v (stderr %q)", err, stderr)
	}
	if !strings.Contains(stderr, "effective config") {
		t.Errorf("-vv stderr = %q, want the effective-config debug line", stderr)
	}

	_, stderr, err = execCmd(t, "bump", "--dry-run", "--overlay", ov, "-v", "dev-util/foo")
	if err != nil {
		t.Fatalf("bump -v: %v", err)
	}
	if strings.Contains(stderr, "effective config") {
		t.Errorf("-v must not emit debug output; stderr = %q", stderr)
	}
}

func TestQuietSuppressesUpstreamResults(t *testing.T) {
	ov := verbosityOverlay(t)
	stubUpstreamResults(t, []upstream.UpstreamResult{{
		Package: "dev-util/foo",
		Current: "1.0.0",
		Latest:  "2.0.0",
		Status:  upstream.StatusOutdated,
	}}, nil)

	stdout, _, err := execCmd(t, "upstream", "--overlay", ov)
	if err != nil {
		t.Fatalf("default run: %v", err)
	}
	if !strings.Contains(stdout, "dev-util/foo") {
		t.Fatalf("default run missing result: %q", stdout)
	}

	stdout, stderr, err := execCmd(t, "upstream", "--overlay", ov, "--quiet")
	if err != nil {
		t.Fatalf("quiet run: %v", err)
	}
	if stdout != "" || stderr != "" {
		t.Errorf("--quiet wrote stdout=%q stderr=%q, want both empty", stdout, stderr)
	}
}

func TestRootDryRunReachesBump(t *testing.T) {
	ov := verbosityOverlay(t)
	stdout, stderr, err := execCmd(t, "bump", "-n", "--overlay", ov, "dev-util/foo")
	if err != nil {
		t.Fatalf("bump -n: %v (stderr %q)", err, stderr)
	}
	if !strings.Contains(stdout, "dry-run:") {
		t.Errorf("bump -n stdout = %q, want dry-run output", stdout)
	}

	for _, name := range []string{"bump", "import", "prepare"} {
		c, _, err := rootCmd.Find([]string{name})
		if err != nil {
			t.Fatalf("find %s: %v", name, err)
		}
		if c.LocalFlags().Lookup("dry-run") != nil {
			t.Errorf("%s still defines a local --dry-run flag", name)
		}
		if c.Flags().ShorthandLookup("n") == nil {
			t.Errorf("%s does not inherit -n from the root --dry-run flag", name)
		}
	}
}

func TestGitHubTokenFlagSetsEnv(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	ov := verbosityOverlay(t)
	if _, stderr, err := execCmd(t, "bump", "--dry-run", "--overlay", ov,
		"--github-token", "tok-123", "dev-util/foo"); err != nil {
		t.Fatalf("execCmd: %v (stderr %q)", err, stderr)
	}
	if got := os.Getenv("GITHUB_TOKEN"); got != "tok-123" {
		t.Errorf("GITHUB_TOKEN = %q, want tok-123", got)
	}
}
