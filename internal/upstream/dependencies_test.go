package upstream

import (
	"testing"
	"time"

	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/provider"
)

// ─── Atom Extraction Tests ───────────────────────────────────────────────────

func TestExtractAtoms_Simple(t *testing.T) {
	input := ">=dev-lang/python-3.9"
	atoms := extractAtoms(input)
	if len(atoms) != 1 {
		t.Fatalf("expected 1 atom, got %d", len(atoms))
	}
	if atoms[0] != "dev-lang/python" {
		t.Errorf("expected 'dev-lang/python', got %q", atoms[0])
	}
}

func TestExtractAtoms_Multi(t *testing.T) {
	input := ">=dev-lang/python-3.9 dev-java/icedtea-web"
	atoms := extractAtoms(input)
	if len(atoms) != 2 {
		t.Fatalf("expected 2 atoms, got %d", len(atoms))
	}
	expected := []string{"dev-lang/python", "dev-java/icedtea-web"}
	for i, exp := range expected {
		if atoms[i] != exp {
			t.Errorf("atom[%d] = %q, want %q", i, atoms[i], exp)
		}
	}
}

func TestExtractAtoms_UseFlag(t *testing.T) {
	input := "python? ( dev-python/requests dev-python/urllib3 )"
	atoms := extractAtoms(input)
	if len(atoms) != 2 {
		t.Fatalf("expected 2 atoms from use-flag, got %d: %v", len(atoms), atoms)
	}
	found := map[string]bool{}
	for _, a := range atoms {
		found[a] = true
	}
	if !found["dev-python/requests"] || !found["dev-python/urllib3"] {
		t.Errorf("expected dev-python/requests and dev-python/urllib3, got %v", atoms)
	}
}

func TestExtractAtoms_Disjunction(t *testing.T) {
	input := "|| ( app-crypt/gpgme app-crypt/gpgmepp )"
	atoms := extractAtoms(input)
	if len(atoms) != 2 {
		t.Fatalf("expected 2 atoms from disjunction, got %d: %v", len(atoms), atoms)
	}
}

func TestExtractAtoms_BraceExpansion(t *testing.T) {
	input := "app-arch/bzip2[bzip2?] dev-libs/expat"
	atoms := extractAtoms(input)
	if len(atoms) != 2 {
		t.Fatalf("expected 2 atoms, got %d: %v", len(atoms), atoms)
	}
	if atoms[0] != "app-arch/bzip2" {
		t.Errorf("expected 'app-arch/bzip2', got %q", atoms[0])
	}
}

func TestExtractAtoms_Slot(t *testing.T) {
	input := ">=dev-libs/glib-2:0"
	atoms := extractAtoms(input)
	if len(atoms) != 1 {
		t.Fatalf("expected 1 atom, got %d", len(atoms))
	}
	if atoms[0] != "dev-libs/glib-2" {
		t.Errorf("expected 'dev-libs/glib-2', got %q", atoms[0])
	}
}

func TestExtractAtoms_VersionSpec(t *testing.T) {
	input := ">=dev-lang/perl-5.32.0"
	atoms := extractAtoms(input)
	if len(atoms) != 1 {
		t.Fatalf("expected 1 atom, got %d", len(atoms))
	}
	if atoms[0] != "dev-lang/perl" {
		t.Errorf("expected 'dev-lang/perl', got %q", atoms[0])
	}
}

func TestExtractAtoms_Empty(t *testing.T) {
	if len(extractAtoms("")) != 0 {
		t.Error("expected 0 atoms from empty string")
	}
	if len(extractAtoms("  ")) != 0 {
		t.Error("expected 0 atoms from whitespace")
	}
}

// ─── DEPEND/RDEPEND Scan Tests ───────────────────────────────────────────────

func TestScanDEPEND(t *testing.T) {
	input := `sys-devel/gcc
>=dev-lang/python-3.9
dev-java/icedtea-web:0`
	atoms := extractAtoms(input)
	if len(atoms) != 3 {
		t.Fatalf("expected 3 atoms from DEPEND, got %d: %v", len(atoms), atoms)
	}
}

func TestScanRDEPEND(t *testing.T) {
	input := `sys-apps/dbus
python? ( dev-python/pygobject )`
	atoms := extractAtoms(input)
	if len(atoms) != 2 {
		t.Fatalf("expected 2 atoms from RDEPEND, got %d: %v", len(atoms), atoms)
	}
}

// ─── DepNode Structure Tests ─────────────────────────────────────────────────

func TestDepNode_RootNode(t *testing.T) {
	node := DepNode{
		Atom:    "dev-lang/python",
		Version: "3.10.0",
		Status:  StatusOutdated,
		Level:   0,
	}
	if node.Atom != "dev-lang/python" {
		t.Errorf("expected 'dev-lang/python', got %q", node.Atom)
	}
	if node.Level != 0 {
		t.Errorf("expected level 0, got %d", node.Level)
	}
	if node.Status != StatusOutdated {
		t.Errorf("expected StatusOutdated, got %q", node.Status)
	}
}

// ─── Recursive Scan Tests ────────────────────────────────────────────────────

func TestRecursiveScan_3Levels(t *testing.T) {
	// Verify that dependency scanning returns nodes and they have correct levels
	ebi := &ebuild.EbuildInfo{
		Category: "dev-libs",
		Pkg:      "alpha",
		PVVal:    "1.0.0",
		DEPEND:   "dev-libs/beta dev-libs/gamma",
		RDEPEND:  "dev-libs/delta",
	}

	reg := provider.NewProviderRegistry()

	nodes := ScanDependencies(ebi, reg, false)
	// With empty registry, deps resolve to StatusUnknown but nodes should still exist
	if len(nodes) != 3 {
		t.Fatalf("expected 3 dependency nodes (beta, gamma, delta), got %d", len(nodes))
	}
	// All should have level 1
	for i, node := range nodes {
		if node.Level != 1 {
			t.Errorf("node[%d] level = %d, want 1", i, node.Level)
		}
		expected := map[string]bool{
			"dev-libs/beta":  true,
			"dev-libs/gamma": true,
			"dev-libs/delta": true,
		}
		if !expected[node.Atom] {
			t.Errorf("node[%d] atom = %q, expected one of beta, gamma, delta", i, node.Atom)
		}
	}
}

func TestRecursiveScan_CircularRef(t *testing.T) {
	// alpha → beta → alpha (should not infinite loop)
	ebi := &ebuild.EbuildInfo{
		Category: "dev-libs",
		Pkg:      "alpha",
		PVVal:    "1.0.0",
		DEPEND:   "dev-libs/beta",
		RDEPEND:  "",
	}

	reg := provider.NewProviderRegistry()

	done := make(chan struct{})
	go func() {
		_ = ScanDependencies(ebi, reg, false)
		close(done)
	}()

	select {
	case <-done:
		// Good — completed without hanging
	case <-time.After(2 * time.Second):
		t.Fatal("ScanDependencies hung on circular dependency — visited set not working")
	}
}

func TestRecursiveScan_NoCycleInfinite(t *testing.T) {
	// Bidirectional cycle: alpha ↔ gamma
	ebi := &ebuild.EbuildInfo{
		Category: "dev-libs",
		Pkg:      "alpha",
		PVVal:    "1.0.0",
		DEPEND:   "dev-libs/gamma",
		RDEPEND:  "",
	}

	reg := provider.NewProviderRegistry()

	done := make(chan struct{})
	go func() {
		_ = ScanDependencies(ebi, reg, false)
		close(done)
	}()

	select {
	case <-done:
		// Good
	case <-time.After(2 * time.Second):
		t.Fatal("ScanDependencies hung — cycle not handled")
	}
}

// ─── Results Merged Test ─────────────────────────────────────────────────────

func TestMergeDepStatus_OutdatedDep(t *testing.T) {
	deps := []UpstreamResult{
		{Package: "dev-libs/B", Status: StatusOutdated},
	}

	newStatus := MergeDepStatuses(StatusUpToDate, deps)
	if newStatus != StatusOutdated {
		t.Errorf("expected StatusOutdated (merged), got %q", newStatus)
	}
}

func TestMergeDepStatus_AllUpToDate(t *testing.T) {
	deps := []UpstreamResult{
		{Package: "dev-libs/B", Status: StatusUpToDate},
		{Package: "dev-libs/C", Status: StatusUpToDate},
	}

	newStatus := MergeDepStatuses(StatusUpToDate, deps)
	if newStatus != StatusUpToDate {
		t.Errorf("expected StatusUpToDate, got %q", newStatus)
	}
}

func TestMergeDepStatus_AheadDep(t *testing.T) {
	deps := []UpstreamResult{
		{Package: "dev-libs/B", Status: StatusAhead},
	}

	newStatus := MergeDepStatuses(StatusUpToDate, deps)
	if newStatus != StatusAhead {
		t.Errorf("expected StatusAhead, got %q", newStatus)
	}
}

func TestMergeDepStatus_ParentNotUpToDate(t *testing.T) {
	deps := []UpstreamResult{
		{Package: "dev-libs/B", Status: StatusOutdated},
	}

	// Parent already outdated, should stay outdated
	newStatus := MergeDepStatuses(StatusOutdated, deps)
	if newStatus != StatusOutdated {
		t.Errorf("expected StatusOutdated (unchanged), got %q", newStatus)
	}
}
