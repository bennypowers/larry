package ebuild

import (
	"os"
	"strings"
)

// ParseEbuildInfo parses an ebuild file and returns EbuildInfo.
func ParseEbuildInfo(path string) (*EbuildInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	info := &EbuildInfo{
		PkgDir: path,
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "CATEGORY=") {
			info.Category = strings.TrimPrefix(line, "CATEGORY=")
			info.Category = strings.Trim(info.Category, `"`)
		}
		if strings.HasPrefix(line, "PV=") {
			info.Version = strings.TrimPrefix(line, "PV=")
			info.Version = strings.Trim(info.Version, `"`)
		}
		if strings.HasPrefix(line, "SRC_URI=") {
			info.SRC_URI = strings.TrimPrefix(line, "SRC_URI=")
			info.SRC_URI = strings.Trim(info.SRC_URI, `"`)
		}
	}

	// Extract package name from path
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		info.Name = strings.TrimSuffix(parts[len(parts)-1], ".ebuild")
	}

	return info, nil
}
