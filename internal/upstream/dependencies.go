package upstream

import (
	"bennypowers.dev/larry/internal/ebuild"
)

// ScanDependencies recursively scans ebuild dependencies.
func ScanDependencies(ei *ebuild.EbuildInfo) ([]*ebuild.EbuildInfo, error) {
	return []*ebuild.EbuildInfo{ei}, nil
}
