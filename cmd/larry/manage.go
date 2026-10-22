package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"bennypowers.dev/larry/internal/cli"
	"bennypowers.dev/larry/internal/upstream"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	manageFresh       bool
	managePrereleases bool
)

// larryExecutable is replaced in tests.
var larryExecutable = os.Executable

func init() {
	rootCmd.AddCommand(&manageCmd)
	manageCmd.Flags().BoolVarP(&manageFresh, "fresh", "f", false,
		"bypass cache and fetch fresh versions")
	manageCmd.Flags().BoolVarP(&managePrereleases, "prereleases", "p", false,
		"include pre-release versions in checks")
}

var manageCmd = cobra.Command{
	Use:   "manage [cat/pkg...] [overlay-path]",
	Short: "Interactive session: check, bump, prepare, and test",
	Long: `Open an interactive upstream session for the overlay.

The selected row supports:
  b  bump the package        p  run prepare        t  run the test pipeline
  R  refresh the check       enter  show details    ?  full key help
  q / Esc / Ctrl+C  quit

Mutating actions ask for confirmation. Their output streams into the view,
and a successful action refreshes the table. Actions run the larry binary as
a child, so behaviour matches the standalone commands.

Requires a terminal. For scripted or piped use, run 'larry upstream'.`,
	Example: `  larry manage
  larry manage dev-util/cem
  larry manage --fresh --prereleases
  larry manage dev-util/cem /path/to/overlay`,
	Args: cobra.ArbitraryArgs,
	RunE: runManage,
}

func runManage(cmd *cobra.Command, args []string) error {
	if !cli.IsTTY(cmd.OutOrStdout()) || !cli.IsTTY(os.Stdin) {
		return fmt.Errorf("manage requires a terminal; use 'larry upstream' for plain output")
	}

	overlayDir, targets, registry, err := resolveUpstreamArgs(args)
	if err != nil {
		return err
	}

	runOpts := upstream.RunOptions{
		OverlayDir:       overlayDir,
		Registry:         registry,
		Targets:          targets,
		AllowPrereleases: managePrereleases,
		Fresh:            manageFresh,
		CacheDir:         upstream.VersionCacheDir(cfg.ResolveCacheDir(rootFlags.CacheDir)),
		Jobs:             cfg.Jobs,
	}

	ctx, cancel := context.WithCancel(context.Background())
	m := cli.NewUpstreamModel(cli.UpstreamModelOptions{
		Context: ctx,
		Fetch: func(ctx context.Context, progress func(upstream.Progress)) ([]upstream.UpstreamResult, error) {
			opts := runOpts
			opts.Context = ctx
			opts.Progress = progress
			return runUpstreamWithTargets(opts)
		},
		// The session is for acting on packages: show everything, with the
		// actionable rows sorted to the top by the model.
		ShowAll: true,
		Theme: cli.NewTheme(cli.ThemeOptions{
			Color:      color.Enabled(),
			Hyperlinks: color.Enabled(),
			Renderer:   lipgloss.NewRenderer(cmd.OutOrStdout()),
		}),
		Overlay: overlayDir,
		Cancel:  cancel,
		Actions: buildUpstreamActions(overlayDir, cfg.DryRun),
		Animate: true,
		DryRun:  cfg.DryRun,
	})
	_, runErr := tea.NewProgram(m, tea.WithAltScreen(), tea.WithOutput(cmd.OutOrStdout())).Run()
	cancel()
	if runErr != nil {
		return runErr
	}
	return m.Err()
}

// buildUpstreamActions exposes the CLI commands as in-session actions. Each
// runs the larry binary as a child so flag parsing, logging, and exit codes
// stay identical to the standalone command, with output streamed line by line.
func buildUpstreamActions(overlay string, dryRun bool) cli.UpstreamActions {
	run := func(command string, withProgress bool, extra ...string) cli.ActionFunc {
		return func(ctx context.Context, pkg string, onLine func(string), onProgress func(cli.ActionProgress)) error {
			args := childCommandArgs(command, overlay, pkg, dryRun, withProgress, extra)
			lines := func(line string) { onLine(redactOverlayPath(line, overlay)) }
			if withProgress {
				return runLarryChildWithProgress(ctx, args, lines, onProgress)
			}
			return runLarryChild(ctx, args, lines)
		}
	}
	testAction := func(ctx context.Context, pkg string, opts cli.TestOptions,
		onLine func(string), onProgress func(cli.ActionProgress)) error {
		var extra []string
		switch opts.Mode {
		case "compile":
			extra = append(extra, "--compile")
		case "merge":
			extra = append(extra, "--merge")
		case "phases":
			for _, phase := range opts.Phases {
				extra = append(extra, "--phase", phase)
			}
		}
		if opts.Use != "" {
			extra = append(extra, "--use", opts.Use)
		}
		args := childCommandArgs("test", overlay, pkg, dryRun, true, extra)
		return runLarryChildWithProgress(ctx, args,
			func(line string) { onLine(redactOverlayPath(line, overlay)) }, onProgress)
	}
	return cli.UpstreamActions{
		Bump:    run("bump", true),
		Prepare: run("prepare", true),
		Test:    testAction,
	}
}

// childCommandArgs builds the argv for one in-session action. Scenario flags
// come first, then the progress channel and global options, then the package.
func childCommandArgs(command, overlay, pkg string, dryRun, withProgress bool, extra []string) []string {
	args := []string{command}
	args = append(args, extra...)
	if withProgress {
		args = append(args, "--progress", "json")
	}
	args = append(args, "--overlay", overlay, "--color", "never")
	if dryRun {
		args = append(args, "--dry-run")
	}
	return append(args, pkg)
}

// redactOverlayPath strips the overlay's absolute root from child output so
// the pane shows repository-relative paths instead of the caller's home dir.
func redactOverlayPath(line, overlay string) string {
	if overlay == "" {
		return line
	}
	cleaned := strings.TrimSuffix(overlay, "/")
	if cleaned == "" {
		return line
	}
	line = strings.ReplaceAll(line, cleaned+"/", "")
	if line == cleaned {
		return "."
	}
	return line
}

// runLarryChild executes larry with args, forwarding stdout and stderr lines.
func runLarryChild(ctx context.Context, args []string, onLine func(string)) error {
	return runLarryChildFunc(ctx, args, onLine)
}

// runLarryChildWithProgress splits structured progress lines out of the child's
// stderr before they reach the log pane.
func runLarryChildWithProgress(ctx context.Context, args []string,
	onLine func(string), onProgress func(cli.ActionProgress)) error {
	return runLarryChildFunc(ctx, args, func(line string) {
		if progress, ok := parseTestProgress(line); ok {
			if onProgress != nil {
				onProgress(progress)
			}
			return
		}
		onLine(line)
	})
}

// parseTestProgress decodes a hidden --progress=json line from the test child.
func parseTestProgress(line string) (cli.ActionProgress, bool) {
	if !strings.HasPrefix(line, progressPrefix) {
		return cli.ActionProgress{}, false
	}
	var wire actionProgressWire
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, progressPrefix)), &wire); err != nil {
		return cli.ActionProgress{}, false
	}
	progress := cli.ActionProgress{Steps: wire.Steps, Step: wire.Step, Message: wire.Message}
	switch wire.State {
	case stepRunning:
		progress.State = cli.ActionStepRunning
	case stepDone:
		progress.State = cli.ActionStepDone
	case stepFailed:
		progress.State = cli.ActionStepFailed
	default:
		progress.State = cli.ActionStepPending
	}
	switch wire.Kind {
	case progressDone:
		progress.Done = true
	case progressError:
		progress.Failed = true
		if wire.Error != "" {
			progress.Err = fmt.Errorf("%s", wire.Error)
		}
	}
	return progress, true
}

func runLarryChildFunc(ctx context.Context, args []string, onLine func(string)) error {
	exe, err := larryExecutable()
	if err != nil {
		return fmt.Errorf("resolve larry executable: %w", err)
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = os.Environ()
	configureProcessGroup(cmd)

	// Keep stdout and stderr apart. os.Pipe (not io.Pipe) matters: os/exec
	// does not spawn copy goroutines for *os.File writers, so cmd.Run returns
	// when the child exits even if a grandchild still holds a pipe fd.
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		_ = stdoutR.Close()
		return fmt.Errorf("stderr pipe: %w", err)
	}
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW

	var wg sync.WaitGroup
	wg.Add(2)
	var mu sync.Mutex
	read := func(r *os.File) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			mu.Lock()
			onLine(line)
			mu.Unlock()
		}
		// A scanner error (for example an over-long line) stops reads; drain
		// the pipe so the child's writer never blocks and cmd.Run can return.
		if err := scanner.Err(); err != nil && !errors.Is(err, os.ErrClosed) {
			mu.Lock()
			onLine("output scanner: " + err.Error())
			mu.Unlock()
		}
		_, _ = io.Copy(io.Discard, r)
	}
	go read(stdoutR)
	go read(stderrR)

	if err := cmd.Start(); err != nil {
		_ = stdoutW.Close()
		_ = stderrW.Close()
		_ = stdoutR.Close()
		_ = stderrR.Close()
		return fmt.Errorf("start: %w", err)
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()

	var runErr error
	select {
	case runErr = <-wait:
	case <-ctx.Done():
		// Kill the whole process group: pkgdev and podman children hold the
		// work open long after the direct child notices the signal.
		terminateProcessGroup(cmd, false)
		select {
		case runErr = <-wait:
		case <-time.After(2 * time.Second):
			terminateProcessGroup(cmd, true)
			runErr = <-wait
		}
	}
	_ = stdoutW.Close()
	_ = stderrW.Close()

	// Wait returns when the child exits, but grandchildren inherit the pipe
	// write ends. If one outlives the child (an orphaned pkgdev, for example),
	// waiting for EOF would leave the action stuck forever. Close the read ends
	// after a short drain so the session always regains control.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = stdoutR.Close()
		_ = stderrR.Close()
		<-done
	}
	return runErr
}
