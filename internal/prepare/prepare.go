package prepare

import (
	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/provider"
)

// Prepare handles the ebuild preparation pipeline.
func Prepare(ei *ebuild.EbuildInfo, p provider.UpstreamProvider) error {
	detected, err := ebuild.DetectType(ei.SRC_URI)
	if err != nil {
		return err
	}
	switch detected {
	case ebuild.TypeNPM:
		return npmPipeline(ei)
	case ebuild.TypeCargo:
		return cargoPipeline(ei)
	case ebuild.TypeGo:
		return goPipeline(ei)
	case ebuild.TypeRpiKernel:
		return rpiKernelPipeline(ei)
	default:
		return generic(ei)
	}
}
