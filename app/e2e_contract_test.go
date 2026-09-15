package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2EAgentContract is the single test that protects the Claude Code /
// planning-plugin integration: the plugin invokes revdiff as
// `revdiff --only=<abs plan path> --output=<tmpfile> --wrap`, then parses the
// markdown annotation records it writes out of that file (or, when no
// --output is given, out of stdout). If the record format, the output
// plumbing, or the exit code drifts, the plugin breaks silently with no
// other test catching it.
//
// It is not guarded by a build tag or testing.Short(): a guarded test CI
// never enables protects nothing.
//
// Mechanism: driving the built binary through a pipe does not work — Bubble
// Tea needs a TTY and a piped stdin cannot deliver keystrokes to it.
// Upstream's own deleted plugin_exit_code_test.go sidestepped this with a
// fake shell script on PATH standing in for ydiff, which never drove the
// real TUI and so was not a real contract test. A PTY dependency
// (e.g. creack/pty) is not in go.mod, and this repo vendors its
// dependencies, so this test instead runs the real model in-process via
// tea.NewProgram(entryModel, tea.WithInput(scriptedInput),
// tea.WithoutRenderer()), feeding keystrokes that open a file, annotate a
// line, and quit — exercising main.go's real composition (buildEntryModel)
// and the real annotation-output pipeline (writeAnnotationOutput), with only
// the terminal I/O substituted.
func TestE2EAgentContract(t *testing.T) {
	t.Run("output file contains exactly the expected markdown record", func(t *testing.T) {
		env := newContractEnv(t)
		outPath := filepath.Join(env.dir, "annotations.md")
		opts := env.parseArgs(t, "--only="+env.filePath, "--wrap", "--output="+outPath)

		result := runScriptedReview(t, opts, annotateSecondLineScript("looks good, ship it"))

		assert.Empty(t, result.stdout, "annotations went to --output, not stdout")
		got, err := os.ReadFile(outPath) //nolint:gosec // test reads a file created under t.TempDir
		require.NoError(t, err)
		want := "## " + env.filePath + ":2 ( )\nlooks good, ship it\n"
		assert.Equal(t, want, string(got), "output file must contain exactly the expected record, headers included")
		assert.Equal(t, want, result.output)
	})

	t.Run("stdout carries the annotation when no --output is given", func(t *testing.T) {
		env := newContractEnv(t)
		opts := env.parseArgs(t, "--only="+env.filePath, "--wrap")

		result := runScriptedReview(t, opts, annotateSecondLineScript("needs a comment here"))

		want := "## " + env.filePath + ":2 ( )\nneeds a comment here\n"
		assert.Equal(t, want, result.stdout, "stdout is the path an agent reads when no --output is given")
		assert.Equal(t, want, result.output)
	})

	t.Run("exit code 10 with --exit-code-on-annotations when annotations exist", func(t *testing.T) {
		env := newContractEnv(t)
		opts := env.parseArgs(t, "--only="+env.filePath, "--wrap", "--exit-code-on-annotations")

		result := runScriptedReview(t, opts, annotateSecondLineScript("flag this"))

		assert.Equal(t, exitCodeAnnotations, result.code)
		assert.NotEmpty(t, result.output)
	})

	t.Run("exit code 0 with --exit-code-on-annotations when no annotations were made", func(t *testing.T) {
		env := newContractEnv(t)
		opts := env.parseArgs(t, "--only="+env.filePath, "--wrap", "--exit-code-on-annotations")

		// no annotation: just quit immediately once the file has loaded.
		result := runScriptedReview(t, opts, []byte("q"))

		assert.Equal(t, 0, result.code)
		assert.Empty(t, result.output)
		assert.Empty(t, result.stdout)
	})
}

// annotateSecondLineScript is the scripted keystroke sequence shared by the
// record-format and stdout subtests: move the cursor down to the file's
// second line ("j"), open the line-annotation input (enter), type comment,
// confirm it (enter), then quit ("q"). All bytes are plain ASCII, which the
// vendored Bubble Tea input decoder maps directly to KeyRunes / KeyEnter
// without needing any terminal escape sequences.
func annotateSecondLineScript(comment string) []byte {
	var b strings.Builder
	b.WriteString("j")
	b.WriteByte('\r')
	b.WriteString(comment)
	b.WriteByte('\r')
	b.WriteString("q")
	return []byte(b.String())
}

// contractEnv is one hermetic, isolated environment for a single subtest:
// its own HOME (so history auto-save and config/theme lookups never touch
// the real ~/.config/ydiff/), its own working directory outside any git
// repository (so --only routes through diff.FileReader's plain-context path
// instead of accidentally picking up the ydiff repo this test itself lives
// in), and its own single-file fixture.
type contractEnv struct {
	dir      string
	filePath string
}

// newContractEnv sets up one contractEnv under fresh t.TempDir()s, redirects
// HOME, and chdirs the process into the hermetic working directory for the
// duration of the calling subtest (restored via t.Cleanup).
func newContractEnv(t *testing.T) contractEnv {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)

	prevWD, err := os.Getwd()
	require.NoError(t, err)
	// read the checked-in fixture relative to the package directory before
	// chdir-ing away from it below.
	src, err := os.ReadFile(filepath.Join(prevWD, "testdata", "e2e_contract", "notes.txt"))
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(prevWD))
	})

	filePath := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(filePath, src, 0o600))

	return contractEnv{dir: dir, filePath: filePath}
}

// parseArgs runs the real CLI parser (parseArgs, the same one main() uses)
// over args, so the test exercises real flag parsing rather than
// hand-building an options struct that could drift from what --only,
// --output, --wrap and --exit-code-on-annotations actually produce.
func (env contractEnv) parseArgs(t *testing.T, args ...string) options {
	t.Helper()
	opts, err := parseArgs(args)
	require.NoError(t, err)
	return opts
}

// scriptedProgramResult is what one scripted run of the review TUI produced:
// the same exit code and stdout text writeAnnotationOutput would hand back
// to main() for a real invocation, plus the raw annotation output text (so
// callers can double check it against a separately-written --output file).
type scriptedProgramResult struct {
	code   int
	stdout string
	output string
}

// runScriptedReview drives buildEntryModel's composition -- the exact model
// construction, dispatch, and annotation store a real invocation uses --
// through a real, in-process tea.Program fed the given keystrokes, then runs
// the same post-Run output pipeline main.run() does (unwrapRootModel,
// Store().FormatOutput(), writeAnnotationOutput). Only the terminal I/O half
// of run() is substituted: a scripted keystroke pipe instead of a real TTY,
// and tea.WithoutRenderer() so no rendering is attempted.
//
// Synchronization: keystrokes are written to the input pipe only after a
// tea.WithFilter hook observes a ui-package fileLoadedMsg pass through the
// program once (matched by reflected type name, since the type itself is
// unexported), so "j"/enter can never race the async file-load command
// Init() issues. A generous timeout context bounds the whole run as a
// fail-fast safety net for a regression that stops the file from loading --
// it does not fire, and is not needed, on the happy path.
func runScriptedReview(t *testing.T, opts options, keys []byte) scriptedProgramResult {
	t.Helper()

	build, err := buildEntryModel(opts)
	require.NoError(t, err)

	pr, pw := io.Pipe()

	ready := make(chan struct{})
	var readyOnce sync.Once
	filter := func(_ tea.Model, msg tea.Msg) tea.Msg {
		if msg != nil && strings.Contains(reflect.TypeOf(msg).String(), "fileLoadedMsg") {
			readyOnce.Do(func() { close(ready) })
		}
		return msg
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	p := tea.NewProgram(build.entryModel,
		tea.WithInput(pr),
		tea.WithoutRenderer(),
		tea.WithFilter(filter),
		tea.WithContext(ctx),
	)

	writeErrCh := make(chan error, 1)
	go func() {
		defer pw.Close()
		select {
		case <-ready:
		case <-ctx.Done():
			writeErrCh <- ctx.Err()
			return
		}
		_, werr := pw.Write(keys)
		writeErrCh <- werr
	}()

	finalModel, runErr := p.Run()
	require.NoError(t, runErr)
	require.NoError(t, <-writeErrCh)

	root, ok := unwrapRootModel(finalModel)
	require.True(t, ok, "final model was not a RootModel")

	var output string
	if !root.Discarded() {
		output = root.Store().FormatOutput()
	}

	var stdout bytes.Buffer
	code, err := writeAnnotationOutput(annotationOutputReq{opts: opts, output: output, stdout: &stdout})
	require.NoError(t, err)

	return scriptedProgramResult{code: code, stdout: stdout.String(), output: output}
}
