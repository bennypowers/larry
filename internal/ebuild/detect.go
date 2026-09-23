package ebuild

import (
	"os"
	"strings"
)

// DetectType determines the package type from an ebuild file path.
// It reads the ebuild content and checks for type-indicating patterns:
//   - vendored:  releases/download/${PN}/ in SRC_URI (pre-vendored deps in GH releases)
//   - npm:       mirror://npm/ in SRC_URI
//   - cargo:     CRATES=" variable definition present
//   - go:        inherits go-module eclass
//   - rpi-kernel: inherits linux-info and has _p in PV
//   - generic:   everything else
//
// The force type overrides auto-detection.
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

	content := string(data)

	// Check for vendored: releases/download/${PN}/ in SRC_URI
	if strings.Contains(content, "releases/download/") && strings.Contains(content, "${PN}/") {
		return TypeVendored, nil
	}

	// Check for npm: mirror://npm/ in SRC_URI
	if strings.Contains(content, "mirror://npm/") {
		return TypeNPM, nil
	}

	// Check for cargo: CRATES=" variable definition
	if strings.Contains(content, "CRATES=") {
		return TypeCargo, nil
	}

	// Check for go: inherits go-module
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "INHERITED=") || strings.Contains(line, "inherit go-module") {
			return TypeGo, nil
		}
	}

	// Check for rpi-kernel: inherits linux-info and PV has _p
	inheritsLinuxInfo := false
	var pv string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "inherit linux-info") {
			inheritsLinuxInfo = true
		}
		if strings.HasPrefix(line, "PV=") {
			pv = strings.TrimPrefix(line, "PV=")
			pv = strings.Trim(pv, `"' `)
		}
	}
	if inheritsLinuxInfo && strings.Contains(pv, "_p") {
		return TypeRpiKernel, nil
	}

	return TypeGeneric, nil
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

	// Check for npm: mirror://npm/ in SRC_URI (more specific than vendored)
	if strings.Contains(info.SRC_URI, "mirror://npm/") {
		return TypeNPM, nil
	}

	// Check for cargo: CRATES= variable definition
	if strings.Contains(info.SRC_URI, "CRATES=") {
		return TypeCargo, nil
	}

	// Check for vendored: releases/download/${PN}/ in SRC_URI
	if strings.Contains(info.SRC_URI, "releases/download/") && strings.Contains(info.SRC_URI, "${PN}/") {
		return TypeVendored, nil
	}

	// Check for cargo: CRATES=" variable definition
	if strings.Contains(info.SRC_URI, "CRATES=") {
		return TypeCargo, nil
	}

	// Check for go: inherits go-module (must come before cargo check in Inherit loop)
	for _, inherited := range info.Inherit {
		if inherited == "go-module" {
			return TypeGo, nil
		}
	}

	// Check for cargo: inherits cargo (covers ebuilds that use ${CARGO_CRATE_URIS} instead of literal CRATES=)
	for _, inherited := range info.Inherit {
		if inherited == "cargo" {
			return TypeCargo, nil
		}
	}

	// Check for rpi-kernel: inherits linux-info and PV has _p
	hasLinuxInfo := false
	for _, inherited := range info.Inherit {
		if inherited == "linux-info" {
			hasLinuxInfo = true
			break
		}
	}
	if hasLinuxInfo && strings.Contains(info.PV(), "_p") {
		return TypeRpiKernel, nil
	}

	return TypeGeneric, nil
}
