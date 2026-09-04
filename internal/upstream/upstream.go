package upstream

import (
	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/provider"
)

// CheckUpstream checks if an upstream update is available.
func CheckUpstream(ei *ebuild.EbuildInfo, p provider.UpstreamProvider) (string, bool, error) {
	return "", false, nil
}
