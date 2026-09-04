package version

import "fmt"

// Version represents a semantic version.
type Version struct {
	Major int
	Minor int
	Patch int
	Pre   string
}

// Parse parses a version string like "1.2.3" or "1.2.3-rc1".
func Parse(s string) (*Version, error) {
	var v Version
	_, err := fmt.Sscanf(s, "%d.%d.%d", &v.Major, &v.Minor, &v.Patch)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// String returns the version as a string.
func (v *Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Compare returns -1, 0, or 1 if v is less than, equal to, or greater than other.
func (v *Version) Compare(other *Version) int {
	if v.Major != other.Major {
		return compareInt(v.Major, other.Major)
	}
	if v.Minor != other.Minor {
		return compareInt(v.Minor, other.Minor)
	}
	if v.Patch != other.Patch {
		return compareInt(v.Patch, other.Patch)
	}
	return compareString(v.Pre, other.Pre)
}

func compareInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func compareString(a, b string) int {
	if a == "" && b == "" {
		return 0
	}
	if a == "" {
		return 1 // release > pre-release
	}
	if b == "" {
		return -1
	}
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
