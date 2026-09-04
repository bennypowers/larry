package bump

import "bennypowers.dev/larry/internal/ebuild"

// BumpInfo holds information about a version bump.
type BumpInfo struct {
	OldVersion string
	NewVersion string
	Revision   int
}

// CreateBumpFile creates a bump file for the ebuild.
func CreateBumpFile(ei *ebuild.EbuildInfo, info *BumpInfo) error {
	return nil
}
