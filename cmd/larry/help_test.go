package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func commandByName(t *testing.T, name string) *cobra.Command {
	t.Helper()
	c, _, err := rootCmd.Find([]string{name})
	if err != nil {
		t.Fatalf("find %s: %v", name, err)
	}
	return c
}

func TestEveryCommandHasLongAndExample(t *testing.T) {
	if strings.TrimSpace(rootCmd.Long) == "" {
		t.Error("root has no Long")
	}
	if strings.TrimSpace(rootCmd.Example) == "" {
		t.Error("root has no Example")
	}
	for _, name := range []string{"bump", "import", "prepare", "test", "upstream", "manage"} {
		c := commandByName(t, name)
		if strings.TrimSpace(c.Long) == "" {
			t.Errorf("%s has no Long", name)
		}
		if strings.TrimSpace(c.Example) == "" {
			t.Errorf("%s has no Example", name)
		}
	}
}

func TestHelpNoStaleTokens(t *testing.T) {
	checks := []struct {
		cmd    string
		forbid []string
	}{
		{"bump", []string{"--auto", "-rr", "requires --auto"}},
		{"prepare", []string{"files/prepare.sh", "-n, --dry-run"}},
		{"test", []string{"--pretend/-n", "--container"}},
		{"import", []string{"prepared automatically"}},
	}
	for _, check := range checks {
		c := commandByName(t, check.cmd)
		text := c.Long + "\n" + c.Example
		for _, bad := range check.forbid {
			if strings.Contains(text, bad) {
				t.Errorf("%s help contains stale text %q", check.cmd, bad)
			}
		}
	}
	for _, want := range []string{"--output"} {
		if !strings.Contains(commandByName(t, "upstream").Long, want) {
			t.Errorf("upstream Long does not mention %s", want)
		}
	}
	for _, want := range []string{"bump", "prepare", "test"} {
		if !strings.Contains(commandByName(t, "manage").Long, want) {
			t.Errorf("manage Long does not mention %s", want)
		}
	}
}

var phaseFlagAllowlist = map[string]bool{
	"clean": true, "fetch": true, "unpack": true, "prepare": true,
	"configure": true, "compile": true, "test": true, "install": true,
	"qmerge": true, "package": true, "preinst": true, "postinst": true,
	"prerm": true, "postrm": true, "emerge": true, "pretend": true,
}

func TestHelpMentionsOnlyRealFlags(t *testing.T) {
	flagRe := regexp.MustCompile(`--([a-z][a-z0-9-]*)`)
	for _, name := range []string{"bump", "import", "prepare", "test", "upstream", "manage"} {
		c := commandByName(t, name)
		text := c.Long + "\n" + c.Example
		for _, match := range flagRe.FindAllStringSubmatch(text, -1) {
			flag := match[1]
			if c.Flags().Lookup(flag) != nil || c.InheritedFlags().Lookup(flag) != nil {
				continue
			}
			if name == "test" && phaseFlagAllowlist[flag] {
				continue
			}
			if flag == "show-toplevel" { // git subcommand in prose
				continue
			}
			t.Errorf("%s help mentions unknown flag --%s", name, flag)
		}
	}
}

func TestHelpVerbosityLadder(t *testing.T) {
	help := rootCmd.PersistentFlags().Lookup("verbose").Usage + "\n" + rootCmd.Long
	for _, want := range []string{"-v", "-vv", "-vvv"} {
		if !strings.Contains(help, want) {
			t.Errorf("verbosity help missing %s: %q", want, help)
		}
	}
}

func TestDocsNoStaleStrings(t *testing.T) {
	files := []string{
		filepath.Join("..", "..", "README.md"),
		filepath.Join("..", "..", "docs", "commands.md"),
	}
	manPages, err := filepath.Glob(filepath.Join("..", "..", "docs", "man", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, manPages...)
	forbidden := []string{
		"files/prepare.sh",
		".larry.yaml",
		"--container",
		"-p, --pretend",
		"generate-completion",
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(data)
		for _, bad := range forbidden {
			if strings.Contains(text, bad) {
				t.Errorf("%s contains stale text %q", path, bad)
			}
		}
	}
}

func TestManPageFlagsExist(t *testing.T) {
	flagRe := regexp.MustCompile(`^\*\*(?:-([a-z]), )?--([a-z][a-z0-9-]*)`)
	manPages, err := filepath.Glob(filepath.Join("..", "..", "docs", "man", "larry-*.1.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range manPages {
		base := filepath.Base(path)
		name := strings.TrimSuffix(strings.TrimPrefix(base, "larry-"), ".1.md")
		c, _, err := rootCmd.Find([]string{name})
		if err != nil {
			t.Errorf("%s maps to unknown command %q", base, name)
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			m := flagRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			flag := m[2]
			if flag == "help" { // cobra built-in
				continue
			}
			if c.Flags().Lookup(flag) == nil && c.InheritedFlags().Lookup(flag) == nil {
				t.Errorf("%s documents unknown flag --%s", base, flag)
			}
		}
	}
}

func TestDocsCommandSynopses(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "commands.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "larry upstream [cat/pkg...] [overlay-path]") {
		t.Error("upstream synopsis is not the real invocation form")
	}
}
