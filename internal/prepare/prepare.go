package prepare

import (
	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/provider"
)

// Prepare handles the ebuild preparation pipeline.
func Prepare(ei *ebuild.EbuildInfo, p provider.UpstreamProvider) error {
	switch ebuild.DetectType(ei.SRC_URI) {
	case remoteid.TypeGitHub, remoteid.TypeGitLab, remoteid.TypeCodeberg:
		return generic(ei)
	case remoteid.TypeNPM:
		return npmPipeline(ei)
	default:
		return generic(ei)
	}
}
