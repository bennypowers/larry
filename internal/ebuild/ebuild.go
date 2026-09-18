package ebuild

// PackageType represents the detected type of an ebuild.
type PackageType string

const (
	TypeNPM        PackageType = "npm"
	TypeCargo      PackageType = "cargo"
	TypeGo         PackageType = "go"
	TypeRpiKernel  PackageType = "rpi-kernel"
	TypeVendored   PackageType = "vendored" // vendored deps in GitHub releases (MIXED_DEPS.md)
	TypeGeneric    PackageType = "generic"
	TypeUnknown    PackageType = "unknown"
)

// EbuildInfo holds structured information extracted from an ebuild via
// `ebuild <path> info` output. Variables are expanded by Portage.
type EbuildInfo struct {
	Path      string
	Category  string
	Pkg       string // PN — package name
	PVVal     string // PV — package version
	PVal      string // P — ${PN}-${PV}
	SRC_URI   string
	DEPEND    string
	RDEPEND   string
	HOMEPAGE  string
	Inherit   []string
	InheritRaw string // raw "inherit ..." line content
	Raw       string // raw ebuild file content
}

// PN returns the package name (PN variable).
func (e *EbuildInfo) PN() string {
	if e.Pkg != "" {
		return e.Pkg
	}
	return ""
}

// PV returns the package version.
func (e *EbuildInfo) PV() string {
	if e.PVVal != "" {
		return e.PVVal
	}
	return ""
}

// P returns ${PN}-${PV}.
func (e *EbuildInfo) P() string {
	if e.PVal != "" {
		return e.PVal
	}
	if e.Pkg != "" && e.PVVal != "" {
		return e.Pkg + "-" + e.PVVal
	}
	return ""
}

// PkgDir returns the directory containing the ebuild.
func (e *EbuildInfo) PkgDir() string {
	return e.Category
}
