package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ContainerConfig holds configuration for the container test runner.
type ContainerConfig struct {
	Overlay   string // --overlay flag
	Distdir   string // --distdir flag (default: $DISTDIR or /var/cache/distfiles)
	CacheDir  string // --cache-dir flag (default: $XDG_CACHE_HOME/larry/binpkgs)
	ImageName string // container image name (default: larry-test)
	UseTUI    bool   // when true, pipe subprocess output instead of inheriting
}

// getImageName returns the container image name.
func getImageName(cfg *ContainerConfig) string {
	if cfg != nil && cfg.ImageName != "" {
		return cfg.ImageName
	}
	return "larry-test"
}

// getCacheDir returns the cache directory.
func getCacheDir(cfg *ContainerConfig) string {
	if cfg != nil && cfg.CacheDir != "" {
		return cfg.CacheDir
	}
	// Default: $XDG_CACHE_HOME/larry/binpkgs
	home := os.Getenv("XDG_CACHE_HOME")
	if home == "" {
		home = os.Getenv("HOME") + "/.cache"
	}
	return filepath.Join(home, "larry", "binpkgs")
}

// getEbuildDir returns the directory containing the ebuild.
func getEbuildDir(ebuildPath string) string {
	if strings.HasSuffix(ebuildPath, ".ebuild") {
		return filepath.Dir(ebuildPath)
	}
	return ebuildPath
}

// getContainerMounts returns the podman mount flags.
func getContainerMounts(cfg *ContainerConfig) []string {
	var mounts []string

	if cfg.Overlay != "" {
		mounts = append(mounts, fmt.Sprintf("--mount=type=bind,source=%s,target=/overlay", cfg.Overlay))
	}
	if cfg.Distdir != "" {
		mounts = append(mounts, fmt.Sprintf("--mount=type=bind,source=%s,target=/distfiles", cfg.Distdir))
	}
	return mounts
}

// getGPUFlags returns GPU passthrough flags for ROCm workstation.
func getGPUFlags(cfg *ContainerConfig) []string {
	return []string{
		"--device=/dev/kfd",
		"--device=/dev/dri/card0",
		"--device=/dev/dri/renderD128",
		"--group-add=video",
	}
}

// ---------------------------------------------------------------------------
// Image build
// ---------------------------------------------------------------------------

// buildImage builds the container image from Containerfile.
// Skips if already cached.
func buildImage(containerfile string, cfg *ContainerConfig, runner CmdRunner) error {
	// Check if Containerfile exists
	if _, err := os.Stat(containerfile); err != nil {
		return fmt.Errorf("Containerfile not found: %s (create with 'larry container init')", containerfile)
	}

	imageName := getImageName(cfg)
	cacheDir := getCacheDir(cfg)
	cacheFile := filepath.Join(cacheDir, imageName)

	// Check if already built
	if _, err := os.Stat(cacheFile); err == nil {
		return nil // cached
	}

	// Build image
	args := []string{"build", "-t", imageName, "-f", containerfile, "."}
	if err := runner.Run("podman", args...); err != nil {
		return fmt.Errorf("podman build failed: %w", err)
	}

	// Mark as cached
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	return os.WriteFile(cacheFile, []byte("built"), 0644)
}

// ---------------------------------------------------------------------------
// Container run
// ---------------------------------------------------------------------------

// runContainerEmerge runs emerge inside the container.
func runContainerEmerge(ebuildPath string, cfg *ContainerConfig, runner CmdRunner, pretend bool) error {
	imageName := getImageName(cfg)

	// Build command
	var args []string
	args = append(args, "run", "--rm")

	// Add mounts
	args = append(args, getContainerMounts(cfg)...)

	// Add GPU passthrough
	args = append(args, getGPUFlags(cfg)...)

	// TUI stream isolation
	if cfg != nil && cfg.UseTUI {
		args = append(args, "--tty=false")
	}

	// Add environment
	args = append(args, "-e", "DISTDIR=/distfiles")
	args = append(args, "-e", "PKGDIR=/binpkgs")

	// Image and command
	args = append(args, imageName)
	args = append(args, "emerge", "--verbose", "--autounmask=y", "=", ebuildPath)

	cmdStr := "podman " + strings.Join(args, " ")

	if pretend {
		fmt.Printf("Would run: %s\n", cmdStr)
		return nil
	}

	// Use runner if provided
	if runner != nil {
		return runner.Run("podman", args...)
	}

	// Determine command based on TUI mode
	if cfg != nil && cfg.UseTUI {
		// Pipe output for TUI
		cmd := exec.Command("podman", args...)
		var stdout strings.Builder
		var stderr strings.Builder
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("podman run failed: %s\nstderr: %s\n%w", cmdStr, stderr.String(), err)
		}
		_ = stdout
	} else {
		// Inherit output
		cmd := exec.Command("podman", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("podman run failed: %w", err)
		}
	}

	return nil
}

// checkPodmanAvailable checks if podman is installed and accessible.
func checkPodmanAvailable(runner CmdRunner) error {
	cmd := exec.Command("podman", "--version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("podman not found: install podman or use 'larry test --local' instead")
	}
	return nil
}

// findContainerfile searches for the Containerfile.larry-test.
func findContainerfile(overlay string) string {
	// Search paths
	searchPaths := []string{
		"Containerfile.larry-test",
		filepath.Join(overlay, "Containerfile.larry-test"),
		"/usr/share/larry/Containerfile.larry-test",
	}

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// TestContainer — main entry point
// ---------------------------------------------------------------------------

// TestContainer runs ebuild tests inside a container.
func TestContainer(ebuildPath string, cfg *ContainerConfig, runner CmdRunner, pretend, useTUI bool) error {
	if runner == nil {
		runner = &realRunner{}
	}

	// Check ebuild exists
	if _, err := os.Stat(ebuildPath); err != nil {
		return fmt.Errorf("ebuild not found: %s", ebuildPath)
	}

	// Find Containerfile
	containerfile := findContainerfile(cfg.Overlay)
	if containerfile == "" {
		// Search in ebuild directory
		ebuildDir := getEbuildDir(ebuildPath)
		containerfile = filepath.Join(ebuildDir, "Containerfile.larry-test")
	}

	if containerfile == "" {
		return fmt.Errorf("Containerfile.larry-test not found (create with 'larry container init')")
	}

	// Set defaults
	if cfg == nil {
		cfg = &ContainerConfig{}
	}
	cfg.UseTUI = useTUI

	// Build image
	if err := buildImage(containerfile, cfg, runner); err != nil {
		return fmt.Errorf("build container image: %w", err)
	}

	// Check podman availability
	if err := checkPodmanAvailable(runner); err != nil {
		return err
	}

	// Run emerge in container
	return runContainerEmerge(ebuildPath, cfg, runner, pretend)
}

// realRunner is the real CmdRunner implementation.
type realRunner struct{}

func (r *realRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
