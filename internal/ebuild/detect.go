package ebuild

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// cratesRe matches a literal CRATES= assignment starting at column 0.
var cratesRe = regexp.MustCompile(`(?m)^CRATES=`)

// validPackageTypes lists the accepted package-type strings.
var validPackageTypes = []string{"npm", "cargo", "go", "vendored", "generic"}

// detectFromRaw is the one and only detection algorithm, operating on the
// raw, unexpanded ebuild text. Order: first match wins.
//
//  1. TypeNPM      — strings.Contains(raw, "mirror://npm/") || strings.Contains(raw, "registry.npmjs.org/")
//  2. TypeCargo    — cratesRe.MatchString(raw) || ParseInherit(raw) contains "cargo"
//  3. TypeGo       — ParseInherit(raw) contains "go-module"
//  4. TypeVendored — strings.Contains(raw, "releases/download/${PN}/")
//  5. TypeGeneric  — otherwise
func detectFromRaw(raw string) PackageType {
	// 1. npm: mirror scheme or direct registry URL (overlays moved off
	// mirror://npm/ because Portage cannot resolve it).
	if strings.Contains(raw, "mirror://npm/") || strings.Contains(raw, "registry.npmjs.org/") {
		return TypeNPM
	}

	// 2. cargo: literal CRATES= block at column 0
	if cratesRe.MatchString(raw) {
		return TypeCargo
	}

	// 2b. cargo: inherits cargo eclass
	for _, e := range ParseInherit(raw) {
		if e == "cargo" {
			return TypeCargo
		}
	}

	// 3. go: inherits go-module
	for _, e := range ParseInherit(raw) {
		if e == "go-module" {
			return TypeGo
		}
	}

	// 4. vendored: the single literal substring (with tag segment included)
	if strings.Contains(raw, "releases/download/${PN}/") {
		return TypeVendored
	}

	// 5. generic
	return TypeGeneric
}

// DetectType determines the package type from an ebuild file path.
// It reads the ebuild content and delegates to detectFromRaw.
func DetectType(ebuildPath string) (PackageType, error) {
	return DetectTypeForce(ebuildPath, "")
}

// DetectTypeForce determines the package type, forcing a specific type if specified.
func DetectTypeForce(ebuildPath string, force PackageType) (PackageType, error) {
	if force != "" {
		return force, nil
	}

	data, err := os.ReadFile(ebuildPath)
	if err != nil {
		return TypeUnknown, err
	}

	return detectFromRaw(string(data)), nil
}

// DetectTypeFromInfo determines the package type from parsed EbuildInfo,
// avoiding file reads when info is already available.
func DetectTypeFromInfo(info *EbuildInfo) (PackageType, error) {
	return DetectTypeFromInfoForce(info, "")
}

// DetectTypeFromInfoForce determines the package type from EbuildInfo,
// forcing a specific type if specified.
func DetectTypeFromInfoForce(info *EbuildInfo, force PackageType) (PackageType, error) {
	if force != "" {
		return force, nil
	}

	if info == nil {
		return TypeGeneric, nil
	}

	// If Raw is populated, use it (the single detection path).
	if info.Raw != "" {
		return detectFromRaw(info.Raw), nil
	}

	// Fallback: build a synthetic raw string from SRC_URI + inherit.
	var raw strings.Builder
	raw.WriteString(info.SRC_URI)
	raw.WriteString("\ninherit ")
	raw.WriteString(strings.Join(info.Inherit, " "))
	raw.WriteString("\n")

	return detectFromRaw(raw.String()), nil
}

// ParsePackageType validates a --type value.
// Accepted: npm, cargo, go, vendored, generic.
// "rpi-kernel" → error pointing to .larry/prepare.sh.
// "" → (TypeUnknown, nil). Anything else returns an error listing valid types.
func ParsePackageType(s string) (PackageType, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return TypeUnknown, nil
	}

	// rpi-kernel is explicitly rejected — handled by custom scripts.
	if s == "rpi-kernel" {
		return "", fmt.Errorf("unknown type %q: rpi-kernel ebuilds require a custom prepare script (see .larry/prepare.sh)", s)
	}

	for _, pt := range validPackageTypes {
		if s == pt {
			return PackageType(pt), nil
		}
	}

	return "", fmt.Errorf("unknown type %q: valid types are %s",
		s, strings.Join(validPackageTypes, ", "))
}
