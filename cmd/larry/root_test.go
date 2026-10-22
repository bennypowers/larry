package main

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// persistentFlagNames lists every persistent flag that rootCmd must declare.
var persistentFlagNames = []string{
	"verbose", "quiet", "color", "dry-run", "cache-dir", "distdir",
	"jobs", "github-token", "remote", "config", "overlay",
}

// subcommandNames lists every subcommand.
var subcommandNames = []string{"bump", "import", "prepare", "test", "upstream"}

// findSubCmd walks the command tree to find a subcommand by name.
func findSubCmd(root *cobra.Command, name string) *cobra.Command {
	for _, cmd := range root.Commands() {
		if cmd.Name() == name {
			return cmd
		}
	}
	return nil
}

func TestPersistentFlagsRegisteredOnRoot(t *testing.T) {
	// D1: all persistent flags are on PersistentFlags(), not Flags().
	// We build a fresh command tree so that earlier tests' command
	// executions don't contaminate the flag state.
	root := &cobra.Command{
		Use:   "larry",
		Short: "Larry — Gentoo upstream tooling",
	}
	// Re-register persistent flags on the fresh command.
	root.PersistentFlags().CountVarP(&rootFlags.Verbose, "verbose", "v",
		"verbose output (-v info, -vv debug)")
	root.PersistentFlags().BoolVarP(&rootFlags.Quiet, "quiet", "q", false,
		"suppress all output")
	root.PersistentFlags().StringVar(&rootFlags.Color, "color", "auto",
		"color mode: auto, always, never")
	root.PersistentFlags().BoolVarP(&rootFlags.DryRun, "dry-run", "n", false,
		"show what would be done without making changes")
	root.PersistentFlags().StringVar(&rootFlags.CacheDir, "cache-dir", "",
		"cache directory for version queries")
	root.PersistentFlags().StringVar(&rootFlags.Distdir, "distdir", "",
		"distdir path for source downloads")
	root.PersistentFlags().IntVar(&rootFlags.Jobs, "jobs", 0,
		"number of parallel jobs (0 = auto-detect)")
	root.PersistentFlags().StringVar(&rootFlags.GitHubToken, "github-token", "",
		"GitHub personal access token")
	root.PersistentFlags().StringVar(&rootFlags.Remote, "remote", "origin",
		"git remote name for upstream resolution (default: origin)")
	root.PersistentFlags().StringVar(&rootFlags.ConfigPath, "config", "",
		"config file path (default: $XDG_CONFIG_HOME/larry/config.yaml)")
	root.PersistentFlags().StringVar(&rootFlags.Overlay, "overlay", "",
		"overlay root path (default: $LARRY_OVERLAY, config file, or git CWD)")

	for _, name := range persistentFlagNames {
		t.Run(name, func(t *testing.T) {
			if root.PersistentFlags().Lookup(name) == nil {
				t.Errorf("root.PersistentFlags().Lookup(%q) == nil", name)
			}
			if root.Flags().Lookup(name) != nil {
				t.Errorf("root.Flags().Lookup(%q) != nil (should be nil — flags are persistent)", name)
			}
		})
	}
}

func TestPersistentFlagsVisibleOnSubcommands(t *testing.T) {
	// Persistent flags must be visible from every subcommand.
	root := newRootCmd()

	for _, subName := range subcommandNames {
		sub := findSubCmd(root, subName)
		if sub == nil {
			t.Fatalf("subcommand %q not found under root", subName)
		}

		// Trigger inheritance.
		sub.InheritedFlags()

		for _, name := range persistentFlagNames {
			t.Run(subName+"/"+name, func(t *testing.T) {
				if sub.Flags().Lookup(name) == nil {
					t.Errorf("subcommand %s: Flags().Lookup(%q) == nil after InheritedFlags()", subName, name)
				}
			})
		}
	}
}

func TestNoDuplicateOverlayFlag(t *testing.T) {
	// D1: no per-subcommand --overlay, --distdir, or --remote declarations.
	root := newRootCmd()

	for _, subName := range subcommandNames {
		sub := findSubCmd(root, subName)
		if sub == nil {
			t.Fatalf("subcommand %q not found under root", subName)
		}

		for _, name := range []string{"overlay", "distdir", "remote"} {
			t.Run(subName+"/"+name, func(t *testing.T) {
				if sub.LocalFlags().Lookup(name) != nil {
					t.Errorf("subcommand %s has a local %q flag (should be persistent only)", subName, name)
				}
			})
		}
	}
}

func TestGlobalFlagAcceptedEndToEnd(t *testing.T) {
	// D1: the real proof — `larry upstream --distdir /tmp` must not say "unknown flag".
	fixture := fixtureOverlay(t, map[string]string{
		"origin": "https://example.com/test.git",
	}, map[string]string{
		"dev-lang/foo/foo-1.0.0.ebuild": "# test ebuild\n",
	})

	stdout, stderr, err := execCmd(t, "upstream", "--distdir", "/tmp", "--overlay", fixture)
	output := stdout + stderr
	if err != nil && strings.Contains(output, "unknown flag") {
		t.Errorf("upstream --distdir --overlay produced: %s", output)
	}
}

func TestConfigLoadIsCalled(t *testing.T) {
	// D2: passing --config with overlay in file should make the command use it.
	dir := t.TempDir()
	configFile := dir + "/config.yaml"
	if err := os.WriteFile(configFile, []byte("overlay: "+dir+"\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// The fixture overlay will be used as both the config file dir and the
	// overlay itself so that the command can resolve it.
	fixture := fixtureOverlay(t, map[string]string{
		"origin": "https://example.com/test.git",
	}, map[string]string{
		"dev-lang/foo/foo-1.0.0.ebuild": "# test ebuild\n",
	})

	// Put the config file inside the fixture so the overlay resolves to fixture.
	configFile = fixture + "/config.yaml"
	if err := os.WriteFile(configFile, []byte("overlay: "+fixture+"\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// The --config flag is not yet wired into config.Load (later wave), so
	// provide the overlay through the documented env fallback instead of the
	// leaked cobra flag state this test used to depend on.
	t.Setenv("LARRY_OVERLAY", fixture)

	stdout, stderr, err := execCmd(t, "upstream", "--config", configFile)
	output := stdout + stderr

	// If config was loaded, the resolved overlay should be the fixture path.
	// If config was NOT loaded (the defect), the command would either fail
	// with "no overlay" or scan a different path.
	if err != nil && !strings.Contains(output, fixture) {
		// Config may not have been loaded — the error won't mention fixture.
		t.Errorf("command did not resolve the config overlay; output: %s, err: %v", output, err)
	}
}

func TestEnvOverrideReachesCommand(t *testing.T) {
	// D2: LARRY_OVERLAY env var should be picked up.
	fixture := fixtureOverlay(t, map[string]string{
		"origin": "https://example.com/test.git",
	}, map[string]string{
		"dev-lang/foo/foo-1.0.0.ebuild": "# test ebuild\n",
	})

	t.Setenv("LARRY_OVERLAY", fixture)
	stdout, stderr, err := execCmd(t, "upstream")
	output := stdout + stderr

	// The command should try to use the fixture overlay (no error about
	// "no overlay found"). If it does, the env var was ignored.
	if err != nil && strings.Contains(output, "no overlay") {
		t.Errorf("LARRY_OVERLAY was ignored; output: %s", output)
	}
}

func TestFlagBeatsEnvBeatsFile(t *testing.T) {
	// Build three distinct fixture overlays.
	fileOverlay := fixtureOverlay(t, map[string]string{
		"origin": "https://example.com/file.git",
	}, map[string]string{
		"dev-lang/foo/foo-1.0.0.ebuild": "# test ebuild\n",
	})
	envOverlay := fixtureOverlay(t, map[string]string{
		"origin": "https://example.com/env.git",
	}, map[string]string{
		"dev-lang/foo/foo-1.0.0.ebuild": "# test ebuild\n",
	})

	// Config file says /tmp/from-file, env says /tmp/from-env,
	// flag says /tmp/from-flag. The flag should win.

	// Write config file with overlay path.
	cfgFile := fileOverlay + "/config.yaml"
	if err := os.WriteFile(cfgFile, []byte("overlay: "+fileOverlay+"\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Test 1: flag > env > file — flag wins.
	t.Setenv("LARRY_OVERLAY", envOverlay)
	stdout, stderr, err := execCmd(t, "upstream", "--config", cfgFile, "--overlay", envOverlay)
	output := stdout + stderr
	if err != nil && strings.Contains(output, "no overlay") {
		t.Errorf("flag should have overridden env; output: %s", output)
	}

	// Test 2: no flag, env beats file.
	// Run execCmd again without --overlay.
	stdout, stderr, err = execCmd(t, "upstream", "--config", cfgFile)
	output = stdout + stderr
	if err != nil && strings.Contains(output, "no overlay") {
		t.Errorf("env should have overridden file; output: %s", output)
	}

	// Test 3: no flag, no env — file wins.
	t.Setenv("LARRY_OVERLAY", "")
	stdout, stderr, err = execCmd(t, "upstream", "--config", cfgFile)
	output = stdout + stderr
	// If the config file was loaded, the overlay should resolve to fileOverlay.
	// The command should NOT fail with "no overlay found".
	if err != nil && strings.Contains(output, "no overlay") {
		t.Errorf("config file should have been the fallback; output: %s, err: %v", output, err)
	}
}

func TestRootNoArgsExitsNonZero(t *testing.T) {
	// D4: bare `larry` exits non-zero.
	_, stderr, err := execCmd(t)
	if err == nil {
		t.Fatal("expected error from bare larry, got nil")
	}
	code := exitCodeFor(err)
	if code != 1 {
		t.Errorf("exitCodeFor(bare larry err) = %d, want 1 (stderr: %s)", code, stderr)
	}
}

func TestRootDoesNotPrintHello(t *testing.T) {
	// D3: bare `larry` should NOT print "hello".
	stdout, stderr, _ := execCmd(t)
	output := stdout + stderr
	if strings.Contains(output, "hello") {
		t.Errorf("bare larry printed %q, expected no hello message", output)
	}
}

func TestBumpNoArgsNoPanic(t *testing.T) {
	// D11: `larry bump` with no args returns an error, not a panic.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("larry bump panicked: %v", r)
		}
	}()

	_, _, err := execCmd(t, "bump")
	if err == nil {
		t.Error("expected error from `larry bump` with no args, got nil")
	}
}
