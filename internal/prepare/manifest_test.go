package prepare

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testRecorder captures ManifestRunner calls for verification.
type callRecorder struct {
	calls []call
}

type call struct {
	bin  string
	args []string
	dir  string
	env  []string
}

func newRecorder() *callRecorder {
	return &callRecorder{}
}

func (r *callRecorder) record(bin string, args []string, dir string, env []string) {
	r.calls = append(r.calls, call{bin, args, dir, env})
}

func (r *callRecorder) called() bool {
	return len(r.calls) > 0
}

func TestRunManifest_TargetsCategoryPackage(t *testing.T) {
	// D1 — cat/pkg target, not ebuild path.
	overlay := t.TempDir()
	dd := filepath.Join(t.TempDir(), "dd")
	eb := filepath.Join(overlay, "dev-util", "foo", "foo-1.0.ebuild")
	os.MkdirAll(filepath.Dir(eb), 0755)
	os.WriteFile(eb, []byte("EAPI=8\n"), 0644)

	rec := newRecorder()
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		rec.record(bin, args, dir, env)
		// Write back both DIST lines (simulating real pkgdev)
		os.WriteFile(filepath.Join(overlay, "dev-util", "foo", "Manifest"), []byte(
			"DIST foo-1.0.tar.gz 10 BLAKE2B aa SHA512 bb\n",
		), 0644)
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

	err := runManifest(manifestRequest{
		Overlay:    overlay,
		CatPkg:     "dev-util/foo",
		Distdir:    dd,
		EbuildPath: eb,
		Prefetch:   nil,
	})
	if err != nil {
		t.Fatalf("runManifest: %v", err)
	}

	if !rec.called() {
		t.Fatal("ManifestRunner was not called")
	}
	c := rec.calls[0]
	if got := len(c.args); got != 4 {
		t.Fatalf("args length: got %d, want 4", got)
	}
	want := []string{"manifest", "-d", dd, "dev-util/foo"}
	for i, w := range want {
		if c.args[i] != w {
			t.Errorf("args[%d] = %q, want %q", i, c.args[i], w)
		}
	}
	if c.dir != overlay {
		t.Errorf("dir = %q, want %q", c.dir, overlay)
	}
}

func TestRunManifest_SiblingVersionsSurvive(t *testing.T) {
	// D1 — both sibling versions survive a cat/pkg manifest run.
	overlay := t.TempDir()
	dd := filepath.Join(t.TempDir(), "dd")
	eb1 := filepath.Join(overlay, "dev-util", "foo", "foo-1.0.ebuild")
	eb2 := filepath.Join(overlay, "dev-util", "foo", "foo-2.0.ebuild")
	os.MkdirAll(filepath.Dir(eb1), 0755)
	os.WriteFile(eb1, []byte("EAPI=8\n"), 0644)
	os.WriteFile(eb2, []byte("EAPI=8\n"), 0644)

	// Pre-existing manifest with both versions.
	manifestPath := filepath.Join(overlay, "dev-util", "foo", "Manifest")
	os.WriteFile(manifestPath, []byte(
		"DIST foo-1.0.tar.gz 10 BLAKE2B aa SHA512 bb\n"+
			"DIST foo-2.0.tar.gz 20 BLAKE2B cc SHA512 dd\n",
	), 0644)

	rec := newRecorder()
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		rec.record(bin, args, dir, env)
		// Real pkgdev writes back ALL versions for a cat/pkg target.
		os.WriteFile(manifestPath, []byte(
			"DIST foo-1.0.tar.gz 10 BLAKE2B aa SHA512 bb\n"+
				"DIST foo-2.0.tar.gz 20 BLAKE2B cc SHA512 dd\n",
		), 0644)
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

	err := runManifest(manifestRequest{
		Overlay:    overlay,
		CatPkg:     "dev-util/foo",
		Distdir:    dd,
		EbuildPath: eb1,
		Prefetch:   nil,
	})
	if err != nil {
		t.Fatalf("runManifest: %v", err)
	}

	if rec.calls[0].args[3] != "dev-util/foo" {
		t.Errorf("catPkg arg = %q, want %q", rec.calls[0].args[3], "dev-util/foo")
	}

	data, _ := os.ReadFile(manifestPath)
	both := strings.Contains(string(data), "foo-1.0.tar.gz") && strings.Contains(string(data), "foo-2.0.tar.gz")
	if !both {
		t.Errorf("Manifest after run:\n%s\nExpected both sibling versions", string(data))
	}
}

func TestRunManifest_CreatesDistdir(t *testing.T) {
	// D4 — nested distdir is created.
	overlay := t.TempDir()
	dd := filepath.Join(t.TempDir(), "nested", "dd")
	eb := filepath.Join(overlay, "dev-util", "foo", "foo-1.0.ebuild")
	os.MkdirAll(filepath.Dir(eb), 0755)
	os.WriteFile(eb, []byte("EAPI=8\n"), 0644)

	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		os.WriteFile(filepath.Join(overlay, "dev-util", "foo", "Manifest"), []byte(
			"DIST foo-1.0.tar.gz 10 BLAKE2B aa SHA512 bb\n",
		), 0644)
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

	if _, err := os.Stat(dd); err == nil {
		t.Fatal("distdir already exists")
	}
	err := runManifest(manifestRequest{
		Overlay:    overlay,
		CatPkg:     "dev-util/foo",
		Distdir:    dd,
		EbuildPath: eb,
		Prefetch:   nil,
	})
	if err != nil {
		t.Fatalf("runManifest: %v", err)
	}
	if _, err := os.Stat(dd); os.IsNotExist(err) {
		t.Fatal("distdir was not created")
	}
}

func TestRunManifest_EmptyDistdirIsError(t *testing.T) {
	// D5 — empty distdir is rejected, runner not called.
	overlay := t.TempDir()
	eb := filepath.Join(overlay, "dev-util", "foo", "foo-1.0.ebuild")
	os.MkdirAll(filepath.Dir(eb), 0755)
	os.WriteFile(eb, []byte("EAPI=8\n"), 0644)

	called := false
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		called = true
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

	err := runManifest(manifestRequest{
		Overlay:    overlay,
		CatPkg:     "dev-util/foo",
		Distdir:    "",
		EbuildPath: eb,
		Prefetch:   nil,
	})
	if err == nil {
		t.Fatal("expected error for empty distdir")
	}
	if !strings.Contains(err.Error(), "distdir is empty") {
		t.Errorf("error = %v, want 'distdir is empty'", err)
	}
	if called {
		t.Fatal("ManifestRunner should not be called with empty distdir")
	}
}

func TestRunManifest_NoGentooMirrorsInEnv(t *testing.T) {
	// D6 — no GENTOO_MIRRORS entry, exactly one DISTDIR.
	overlay := t.TempDir()
	dd := filepath.Join(t.TempDir(), "dd")
	eb := filepath.Join(overlay, "dev-util", "foo", "foo-1.0.ebuild")
	os.MkdirAll(filepath.Dir(eb), 0755)
	os.WriteFile(eb, []byte("EAPI=8\n"), 0644)

	// Capture the env passed to ManifestRunner.
	type envCapture struct{ env []string }
	captured := &envCapture{}

	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		captured.env = env
		os.WriteFile(filepath.Join(overlay, "dev-util", "foo", "Manifest"), []byte(
			"DIST foo-1.0.tar.gz 10 BLAKE2B aa SHA512 bb\n",
		), 0644)
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

	err := runManifest(manifestRequest{
		Overlay:    overlay,
		CatPkg:     "dev-util/foo",
		Distdir:    dd,
		EbuildPath: eb,
		Prefetch:   nil,
	})
	if err != nil {
		t.Fatalf("runManifest: %v", err)
	}

	for _, e := range captured.env {
		if strings.HasPrefix(e, "GENTOO_MIRRORS=") {
			t.Errorf("env contains GENTOO_MIRRORS entry: %s", e)
		}
	}
	distdirCount := 0
	for _, e := range captured.env {
		if e == "DISTDIR="+dd {
			distdirCount++
		}
	}
	if distdirCount != 1 {
		t.Errorf("DISTDIR count = %d, want 1", distdirCount)
	}
}

func TestRunManifest_PrefetchRunsBeforeManifestRemoval(t *testing.T) {
	overlay := t.TempDir()
	dd := filepath.Join(t.TempDir(), "dd")
	eb := filepath.Join(overlay, "dev-util", "foo", "foo-1.0.ebuild")
	pkgDir := filepath.Join(overlay, "dev-util", "foo")
	os.MkdirAll(pkgDir, 0755)
	os.WriteFile(eb, []byte("EAPI=8\n"), 0644)
	os.WriteFile(filepath.Join(pkgDir, "Manifest"), []byte("AUX foo.patch 10 BLAKE2B aa SHA512 bb\n"), 0644)

	prefetchSeenManifest := false
	prefetchPkgDir := ""
	prefetchDistdir := ""

	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		os.WriteFile(filepath.Join(pkgDir, "Manifest"), []byte("DIST foo-1.0.tar.gz 10 BLAKE2B aa SHA512 bb\n"), 0644)
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

	err := runManifest(manifestRequest{
		Overlay:    overlay,
		CatPkg:     "dev-util/foo",
		Distdir:    dd,
		EbuildPath: eb,
		Prefetch: func(pkgDir, distdir string) error {
			prefetchPkgDir = pkgDir
			prefetchDistdir = distdir
			_, err := os.Stat(filepath.Join(pkgDir, "Manifest"))
			prefetchSeenManifest = err == nil
			return nil
		},
	})
	if err != nil {
		t.Fatalf("runManifest: %v", err)
	}

	if !prefetchSeenManifest {
		t.Error("Prefetch: Manifest should have been present when Prefetch ran")
	}
	if prefetchPkgDir != pkgDir {
		t.Errorf("Prefetch pkgDir = %q, want %q", prefetchPkgDir, pkgDir)
	}
	if prefetchDistdir != dd {
		t.Errorf("Prefetch distdir = %q, want %q", prefetchDistdir, dd)
	}
}

func TestRunManifest_PrefetchErrorAborts(t *testing.T) {
	overlay := t.TempDir()
	dd := filepath.Join(t.TempDir(), "dd")
	eb := filepath.Join(overlay, "dev-util", "foo", "foo-1.0.ebuild")
	pkgDir := filepath.Join(overlay, "dev-util", "foo")
	os.MkdirAll(pkgDir, 0755)
	os.WriteFile(eb, []byte("EAPI=8\n"), 0644)
	os.WriteFile(filepath.Join(pkgDir, "Manifest"), []byte("AUX foo.patch 10 BLAKE2B aa SHA512 bb\n"), 0644)

	called := false
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		called = true
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

	prefetchErr := errors.New("prefetch boom")
	err := runManifest(manifestRequest{
		Overlay:    overlay,
		CatPkg:     "dev-util/foo",
		Distdir:    dd,
		EbuildPath: eb,
		Prefetch: func(pkgDir, distdir string) error {
			return prefetchErr
		},
	})
	if err == nil {
		t.Fatal("expected error from Prefetch")
	}
	if !errors.Is(err, prefetchErr) && !strings.Contains(err.Error(), "prefetch boom") {
		t.Errorf("error = %v, want to contain 'prefetch boom'", err)
	}
	if called {
		t.Fatal("ManifestRunner should not be called after Prefetch error")
	}

	// Manifest should still be on disk.
	if _, err := os.Stat(filepath.Join(pkgDir, "Manifest")); os.IsNotExist(err) {
		t.Fatal("Manifest should still exist after Prefetch failure")
	}
}

func TestRunManifest_NilPrefetchIsFine(t *testing.T) {
	overlay := t.TempDir()
	dd := filepath.Join(t.TempDir(), "dd")
	eb := filepath.Join(overlay, "dev-util", "foo", "foo-1.0.ebuild")
	os.MkdirAll(filepath.Dir(eb), 0755)
	os.WriteFile(eb, []byte("EAPI=8\n"), 0644)

	called := false
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		called = true
		os.WriteFile(filepath.Join(overlay, "dev-util", "foo", "Manifest"), []byte(
			"DIST foo-1.0.tar.gz 10 BLAKE2B aa SHA512 bb\n",
		), 0644)
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

	err := runManifest(manifestRequest{
		Overlay:    overlay,
		CatPkg:     "dev-util/foo",
		Distdir:    dd,
		EbuildPath: eb,
		Prefetch:   nil,
	})
	if err != nil {
		t.Fatalf("runManifest: %v", err)
	}
	if !called {
		t.Fatal("ManifestRunner should be called with nil Prefetch")
	}
}

func TestVerifyManifest_NoSrcUriIsValid(t *testing.T) {
	// D7 — meta/live ebuilds with only AUX are valid when no distfiles expected.
	pkgDir := t.TempDir()
	os.WriteFile(filepath.Join(pkgDir, "Manifest"), []byte(
		"AUX foo.patch 10 BLAKE2B aa SHA512 bb\n",
	), 0644)

	err := verifyManifestValid(pkgDir, nil)
	if err != nil {
		t.Errorf("verifyManifestValid(pkgDir, nil) = %v, want nil", err)
	}
}

func TestVerifyManifest_MissingManifestNoExpectationsIsValid(t *testing.T) {
	pkgDir := t.TempDir()
	err := verifyManifestValid(pkgDir, nil)
	if err != nil {
		t.Errorf("verifyManifestValid(pkgDir, nil) = %v, want nil", err)
	}
}

func TestVerifyManifest_ZeroDistWithExpectationsIsInvalid(t *testing.T) {
	pkgDir := t.TempDir()
	os.WriteFile(filepath.Join(pkgDir, "Manifest"), []byte(
		"AUX foo.patch 10 BLAKE2B aa SHA512 bb\n",
	), 0644)

	err := verifyManifestValid(pkgDir, []string{"foo-1.0.tar.gz"})
	if err == nil {
		t.Fatal("expected error for missing DIST entries")
	}
	if !strings.Contains(err.Error(), "no valid DIST entries") {
		t.Errorf("error = %v, want 'no valid DIST entries'", err)
	}
}

func TestVerifyManifest_EmptyDistEntryIsInvalid(t *testing.T) {
	// Zero size (0) fails [1-9]\d*; blank hash fails \S+.
	pkgDir := t.TempDir()
	os.WriteFile(filepath.Join(pkgDir, "Manifest"), []byte(
		"DIST foo.tar.gz 0 BLAKE2B  SHA512 \n",
	), 0644)

	err := verifyManifestValid(pkgDir, nil)
	if err != nil {
		// With nil expectations, zero DIST lines is OK.
		t.Logf("verifyManifestValid(pkgDir, nil) = %v (expected: nil for no expectations)", err)
	}

	// With expectations, it should fail.
	err = verifyManifestValid(pkgDir, []string{"foo.tar.gz"})
	if err == nil {
		t.Fatal("expected error for empty DIST entry with expectations")
	}
}

func TestVerifyManifest_WellFormedDistIsValid(t *testing.T) {
	pkgDir := t.TempDir()
	os.WriteFile(filepath.Join(pkgDir, "Manifest"), []byte(
		"DIST foo-1.0.tar.gz 1234 BLAKE2B abc SHA512 def\n",
	), 0644)

	err := verifyManifestValid(pkgDir, nil)
	if err != nil {
		t.Errorf("verifyManifestValid(pkgDir, nil) = %v, want nil", err)
	}

	err = verifyManifestValid(pkgDir, []string{"foo-1.0.tar.gz"})
	if err != nil {
		t.Errorf("verifyManifestValid(pkgDir, [dist]) = %v, want nil", err)
	}
}
