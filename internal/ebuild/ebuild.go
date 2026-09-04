package ebuild

import (
	"bennypowers.dev/larry/internal/remoteid"
)

// EbuildInfo holds information extracted from an ebuild.
type EbuildInfo struct {
	Name    string
	Version string
	PkgDir  string
	SRC_URI string
	Category string
	Remote  *remoteid.RemoteId
}
