package prepare

import "os"

// Stage creates a temporary staging directory for dry-run.
func Stage() (string, error) {
	return os.MkdirTemp("", "larry-staging-*")
}
