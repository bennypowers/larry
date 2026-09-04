package test

import (
	"bennypowers.dev/larry/internal/ebuild"
)

// RunTest runs ebuild tests in the specified mode.
func RunTest(ei *ebuild.EbuildInfo, mode string) error {
	switch mode {
	case "local":
		return RunLocal(ei)
	case "container":
		return RunContainer(ei)
	default:
		return RunLocal(ei)
	}
}
