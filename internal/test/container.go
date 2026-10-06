package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"bennypowers.dev/larry/internal/atom"
)

// ContainerConfig holds configuration for the container test runner.
type ContainerConfig struct {
	Overlay   string // --overlay flag
	Distdir   string // --distdir flag
	CacheDir  string // --cache-dir flag
	ImageName string // --image flag (empty = default "larry-test")
	UseTUI    bool   // when true, pipe subprocess output instead of inheriting
}

// getImageName returns the container image name.
func getImageName(cfg *ContainerConfig) string {
	if cfg != nil && cfg.ImageName != "" {
		return cfg.ImageName
	}
	return "larry-test"
}

// ---------------------------------------------------------------------------
// D7 — getContainerMounts returns podman argv elements, two per mount.
// Switches from --mount=type=bind to -v src:dst[:ro] form.
// ---------------------------------------------------------------------------

func getContainerMounts(cfg *ContainerConfig) []string {
	// Create cache directories first (script L834).
	cacheDir := getCacheDir(cfg)
	if err := os.MkdirAll(filepath.Join(cacheDir, "distfiles"), 0755); err != nil {
		// Best-effort; errors will surface later.
		_ = err
	}
	if err := os.MkdirAll(filepath.Join(cacheDir, "binpkgs"), 0755); err != nil {
		_ = err
	}

	// Resolve repo name from overlay.
	repo := resolveRepoName(cfg.Overlay)

	var mounts []string
	if cfg.Overlay != "" {
		mounts = append(mounts, "-v", cfg.Overlay+":/var/db/repos/"+repo+":ro")
	}
	mounts = append(mounts, "-v", filepath.Join(cacheDir, "distfiles")+":/var/cache/distfiles")
	mounts = append(mounts, "-v", filepath.Join(cacheDir, "binpkgs")+":/var/cache/binpkgs")
	return mounts
}

// ---------------------------------------------------------------------------
// D10 — getGPUFlags returns GPU passthrough flags.
// Changed from --device=/dev/kfd --device=/dev/dri/card0 etc.
// to --device /dev/kfd --device /dev/dri --group-add video
// ---------------------------------------------------------------------------

func getGPUFlags() []string {
	return []string{"--device", "/dev/kfd", "--device", "/dev/dri", "--group-add", "video"}
}

// ---------------------------------------------------------------------------
// D5 — imageExists checks via podman image exists, not a sentinel file.
// ---------------------------------------------------------------------------

func imageExists(name string, runner CmdRunner) bool {
	if runner == nil {
		runner = &realRunner{}
	}
	err := runner.Run("podman", "image", "exists", name)
	return err == nil
}

// ---------------------------------------------------------------------------
// Container run
// ---------------------------------------------------------------------------

// runContainerEmerge runs emerge inside the container.
func runContainerEmerge(ebuildPath string, cfg *ContainerConfig, runner CmdRunner, pretend bool) error {
	imageName := getImageName(cfg)

	// Resolve ebuild path and build atom target.
	pe, err := atom.ParseEbuildPath(ebuildPath)
	if err != nil {
		return fmt.Errorf("parsing ebuild path: %w", err)
	}
	atomTarget := "=" + pe.Category + "/" + pe.PF

	// Build command
	var args []string
	args = append(args, "run", "--rm")

	// Add mounts (D7).
	args = append(args, getContainerMounts(cfg)...)

	// Add GPU passthrough (D10).
	args = append(args, getGPUFlags()...)

	// TUI stream isolation
	if cfg != nil && cfg.UseTUI {
		args = append(args, "--tty=false")
	}

	// Image and command.
	// No -e DISTDIR=/distfiles or -e PKGDIR=/binpkgs — image's make.conf sets both.
	args = append(args, imageName)

	// Emerage args: -1 --verbose --autounmask=y --autounmask-write=y [--pretend] <atom>
	args = append(args, "emerge", "-1", "--verbose", "--autounmask=y", "--autounmask-write=y")
	if pretend {
		args = append(args, "--pretend")
	}
	args = append(args, atomTarget)

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
// D11: now uses the runner instead of exec.Command directly.
func checkPodmanAvailable(runner CmdRunner) error {
	if err := runner.Run("podman", "--version"); err != nil {
		return fmt.Errorf("podman not found: install podman or use 'larry test' local mode instead")
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

// getCacheDir returns the cache directory.
func getCacheDir(cfg *ContainerConfig) string {
	if cfg != nil && cfg.CacheDir != "" {
		return cfg.CacheDir
	}
	// Default: $XDG_CACHE_HOME/larry
	home := os.Getenv("XDG_CACHE_HOME")
	if home == "" {
		home = os.Getenv("HOME") + "/.cache"
	}
	return filepath.Join(home, "larry")
}

// resolveRepoName resolves the repository name from the overlay directory.
// Order: <overlay>/profiles/repo_name (first line, trimmed).
func resolveRepoName(overlay string) string {
	if overlay == "" {
		return ""
	}
	repoFile := filepath.Join(overlay, "profiles", "repo_name")
	if data, err := os.ReadFile(repoFile); err == nil {
		return strings.TrimSpace(string(data))
	}
	return ""
}

// buildImage builds the container image from Containerfile.
// D5: uses podman image exists (not sentinel file).
// D6: build context is overlay, not ".".
func buildImage(containerfile string, overlay, imageName string, runner CmdRunner) error {
	// Check if Containerfile exists
	if _, err := os.Stat(containerfile); err != nil {
		return fmt.Errorf("Containerfile not found: %s (create with 'larry container init')", containerfile)
	}

	// Resolve repo name
	repo := resolveRepoName(overlay)

	// Build image: context = overlay, not "."
	args := []string{"build", "-t", imageName, "-f", containerfile, overlay}
	if repo != "" {
		args = append(args, "--build-arg", "REPO="+repo)
	}
	if err := runner.Run("podman", args...); err != nil {
		return fmt.Errorf("podman build failed: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// TestContainer — main entry point
// ---------------------------------------------------------------------------

// TestRunnerForContainer is an injectable runner for container tests (like TestRunnerForTest).
var TestRunnerForContainer CmdRunner

// TestContainer runs ebuild tests inside a container.
// D4: pretend returns before any podman operations.
// D8: nil guard is the first statement.
func TestContainer(ebuildPath string, cfg *ContainerConfig, runner CmdRunner, pretend, useTUI bool) error {
	// D8: nil guard must be first statement.
	if cfg == nil {
		cfg = &ContainerConfig{}
	}
	cfg.UseTUI = useTUI

	if runner == nil {
		if TestRunnerForContainer != nil {
			runner = TestRunnerForContainer
		} else {
			runner = &realRunner{}
		}
	}

	// Check ebuild exists
	if _, err := os.Stat(ebuildPath); err != nil {
		return fmt.Errorf("ebuild not found: %s", ebuildPath)
	}

	// Resolve cache dir and image name.
	cacheDir := getCacheDir(cfg)
	imageName := getImageName(cfg)

	// Find Containerfile — after nil guard (D8).
	containerfile := findContainerfile(cfg.Overlay)
	if containerfile == "" {
		// Search in ebuild directory
		ebuildDir := getEbuildDir(ebuildPath)
		containerfile = filepath.Join(ebuildDir, "Containerfile.larry-test")
	}
	// D8: deleted unreachable second if containerfile == ""

	// D4: If pretending, print the command and return immediately.
	// No podman check, no image build, no container run.
	if pretend {
		// Resolve atom target for the printed command.
		pe, err := atom.ParseEbuildPath(ebuildPath)
		if err != nil {
			return fmt.Errorf("parsing ebuild path: %w", err)
		}
		atomTarget := "=" + pe.Category + "/" + pe.PF
		fmt.Printf("Would run: podman run --rm --device /dev/kfd --device /dev/dri --group-add video \\\n")
		fmt.Printf("  -v %s:/var/db/repos/%s:ro \\\n", cfg.Overlay, resolveRepoName(cfg.Overlay))
		fmt.Printf("  -v %s/distfiles:/var/cache/distfiles \\\n", cacheDir)
		fmt.Printf("  -v %s/binpkgs:/var/cache/binpkgs \\\n", cacheDir)
		fmt.Printf("  %s emerge -1 --verbose --autounmask=y --autounmask-write=y --pretend %s\n", imageName, atomTarget)
		return nil
	}

	// D4: Check podman availability (now actually uses runner — D11).
	if err := checkPodmanAvailable(runner); err != nil {
		return err
	}

	// D5: Check image existence via podman image exists.
	if !imageExists(imageName, runner) {
		// D5: Image absent — build it.
		if err := buildImage(containerfile, cfg.Overlay, imageName, runner); err != nil {
			return err
		}
	}

	// Run emerge in container.
	return runContainerEmerge(ebuildPath, cfg, runner, false)
}

// realRunner is the real CmdRunner implementation.
type realRunner struct{}

func (r *realRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (r *realRunner) RunWithEnv(env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// getEbuildDir returns the directory containing the ebuild.
func getEbuildDir(ebuildPath string) string {
	if strings.HasSuffix(ebuildPath, ".ebuild") {
		return filepath.Dir(ebuildPath)
	}
	return ebuildPath
}
