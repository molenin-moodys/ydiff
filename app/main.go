package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jessevdk/go-flags"
	"github.com/muesli/termenv"

	"github.com/molenin-moodys/ydiff/app/annotation"
	"github.com/molenin-moodys/ydiff/app/browser"
	"github.com/molenin-moodys/ydiff/app/diff"
	"github.com/molenin-moodys/ydiff/app/fsutil"
	"github.com/molenin-moodys/ydiff/app/gitstate"
	"github.com/molenin-moodys/ydiff/app/highlight"
	"github.com/molenin-moodys/ydiff/app/keymap"
	"github.com/molenin-moodys/ydiff/app/theme"
	"github.com/molenin-moodys/ydiff/app/ui"
	"github.com/molenin-moodys/ydiff/app/ui/overlay"
	"github.com/molenin-moodys/ydiff/app/ui/sidepane"
	"github.com/molenin-moodys/ydiff/app/ui/style"
	"github.com/molenin-moodys/ydiff/app/ui/worddiff"
)

var revision = "unknown"

const exitCodeAnnotations = 10

func main() {
	opts, parseErr := parseArgs(os.Args[1:])
	if parseErr != nil {
		var flagsErr *flags.Error
		if errors.As(parseErr, &flagsErr) && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		}
		if !errors.As(parseErr, &flagsErr) {
			fmt.Fprintf(os.Stderr, "error: %v\n", parseErr)
		}
		os.Exit(1)
	}

	// early-exit commands that don't need theme resolution
	if opts.Version {
		fmt.Printf("version: %s\n", revision)
		os.Exit(0)
	}

	if opts.DumpConfig {
		dumpConfig(os.Args[1:], os.Stdout)
		os.Exit(0)
	}

	if opts.DumpKeys {
		km := keymap.LoadOrDefault(resolveFlagPath(os.Args[1:], "keys", "YDIFF_KEYS", defaultKeysPath))
		if err := km.Dump(os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	themesDir := defaultThemesDir()
	cat := theme.NewCatalog(themesDir)
	done, thErr := handleThemes(&opts, cat, os.Stdout, os.Stderr)
	if thErr != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", thErr)
		os.Exit(1)
	}
	if done {
		os.Exit(0)
	}

	if opts.DumpTheme {
		colors := collectColors(opts)
		th := theme.Theme{Colors: colors, ChromaStyle: opts.ChromaStyle}
		if err := th.Dump(os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	code, err := run(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if code != 0 {
		os.Exit(code)
	}
}

// entryModelResult bundles everything buildEntryModel constructs: the model
// tea.Program should run, the ProgramOptions run() would use for a real
// terminal, and the bookkeeping (gitRoot, workDir) only needed after the
// program exits (history saving). It is a single seam between "build the
// composition" and "drive it with a tea.Program", extracted so a test can
// substitute the terminal I/O half (scripted keystrokes, no renderer) while
// exercising the exact same model construction, dispatch, and annotation
// store that a real invocation uses — see app/e2e_contract_test.go, which
// runs the real model in-process instead of a built binary because Bubble
// Tea needs a TTY and a piped stdin cannot deliver keystrokes to it.
type entryModelResult struct {
	entryModel     tea.Model
	programOptions []tea.ProgramOption
	tty            *os.File // non-nil only in --stdin mode; caller must Close it after the program exits
	gitRoot        string
	workDir        string
}

func buildEntryModel(opts options) (entryModelResult, error) {
	// force lipgloss to truecolor when colors are enabled. revdiff's raw-ANSI
	// helpers (style.ansiColor) always emit truecolor, but lipgloss respects
	// the termenv-detected profile, which can downgrade to ANSI256 / ANSI in
	// tmux or terminals where TERM/COLORTERM detection regresses. The mismatch
	// makes lipgloss-rendered colors (pane borders, file tree fg) look wrong
	// while raw-ANSI paths (line prefix wrap, overlay title injection) render
	// correctly. Forcing truecolor unifies the two paths.
	if !opts.NoColors {
		lipgloss.SetColorProfile(termenv.TrueColor)
	}

	store := annotation.NewStore()
	hl := highlight.New(opts.ChromaStyle, !opts.NoColors)
	km := keymap.LoadOrDefault(resolveKeysPath(opts))

	var (
		renderer           ui.Renderer
		workDir            string
		gitRoot            string
		blamer             ui.Blamer
		untrackedFn        func() ([]string, error)
		untrackedRenamesFn func([]string) ([]diff.FileEntry, error)
		commitLogger       diff.CommitLogger
		vcsType            diff.VCSType
		err                error
	)

	programOptions := []tea.ProgramOption{tea.WithAltScreen()}
	if !opts.NoMouse {
		programOptions = append(programOptions, tea.WithMouseCellMotion())
	}
	description, err := resolveDescription(opts)
	if err != nil {
		return entryModelResult{}, err
	}

	// decideRoute is pure over opts: it never depends on being inside a
	// repository, so it is computed up front and consulted below only to
	// decide whether a "no repository" failure from setupVCSRenderer should
	// be tolerated (browser route) or reported (review route).
	decision := decideRoute(opts)

	var tty *os.File
	switch {
	case opts.compareAbsOld != "":
		renderer = diff.NewCompareReader(opts.compareAbsOld, opts.compareAbsNew)
		workDir = filepath.Dir(opts.compareAbsNew)
	case opts.Stdin:
		var stdinErr error
		renderer, tty, stdinErr = prepareStdinMode(opts, os.Stdin)
		if stdinErr != nil {
			return entryModelResult{}, stdinErr
		}
		programOptions = append(programOptions, tea.WithInput(tty))
	default:
		var setup vcsSetup
		setup, err = vcsSetupForRoute(opts, decision)
		if err != nil {
			return entryModelResult{}, err
		}
		renderer = setup.renderer
		gitRoot = setup.gitRoot
		workDir = setup.workDir
		blamer = setup.blamer
		untrackedFn = filterUntracked(setup.untrackedFn, opts.Include, opts.Exclude)
		untrackedRenamesFn = setup.untrackedRenamesFn
		commitLogger = setup.commitLogger
		vcsType = setup.vcsType
	}

	if opts.Annotations != "" {
		if perr := preloadAnnotations(opts.Annotations, store, renderer, opts.ref(), opts.Staged, untrackedFn, untrackedRenamesFn, workDir, os.Stderr); perr != nil {
			return entryModelResult{}, perr
		}
	}

	// construct the three style types per D15: Resolver first, Renderer from Resolver, SGR is zero-value
	styleColors := optsToStyleColors(opts)
	var res style.Resolver
	if opts.NoColors {
		res = style.PlainResolver()
	} else {
		res = style.NewResolver(styleColors)
	}

	themesDir := defaultThemesDir()
	configPath := resolveFlagPath(os.Args[1:], "config", "YDIFF_CONFIG", defaultConfigPath)
	themes := &themeCatalog{
		catalog:    theme.NewCatalog(themesDir),
		configPath: configPath,
	}

	model, err := ui.NewModel(ui.ModelConfig{
		Renderer:             renderer,
		Store:                store,
		Highlighter:          hl,
		StyleResolver:        res,
		StyleRenderer:        style.NewRenderer(res),
		SGR:                  style.SGR{},
		WordDiffer:           worddiff.New(),
		Overlay:              overlay.NewManager(),
		Themes:               themes,
		Blamer:               blamer,
		LoadUntracked:        untrackedFn,
		LoadUntrackedRenames: untrackedRenamesFn,
		Keymap:               km,
		CommitLog:            commitLogger,
		CommitsApplicable:    commitsApplicable(opts, commitLogger),
		ReloadApplicable:     reloadApplicable(opts),
		CompactApplicable:    compactApplicable(opts, renderer),
		NoColors:             opts.NoColors,
		MouseTracking:        !opts.NoMouse,
		NoStatusBar:          opts.NoStatusBar,
		NoConfirmDiscard:     opts.NoConfirmDiscard,
		NoConfirmReload:      opts.NoConfirmReload,
		Wrap:                 opts.Wrap,
		WrapIndent:           opts.WrapIndent,
		Collapsed:            opts.Collapsed,
		Compact:              opts.Compact,
		CompactContext:       opts.CompactContext,
		CrossFileHunks:       opts.CrossFileHunks,
		LineNumbers:          opts.LineNumbers,
		ShowBlame:            opts.Blame,
		ShowUntracked:        opts.startupUntracked(),
		WordDiff:             opts.WordDiff,
		ReviewInfo: reviewInfoFromOptions(opts, reviewInfoInputs{
			workDir:     workDir,
			vcsType:     vcsType,
			description: description,
		}),
		TabWidth:         opts.TabWidth,
		Ref:              opts.ref(),
		Staged:           opts.Staged,
		TreeWidthRatio:   opts.TreeWidth,
		Only:             opts.Only,
		WorkDir:          workDir,
		SourceEditor:     sourceEditorPolicy(opts, workDir),
		ActiveThemeName:  themes.catalog.ActiveName(opts.Theme),
		AnnotationMarker: opts.AnnotationMarker,
		OutputPath:       opts.Output,
		NewFileTree: func(entries []diff.FileEntry) ui.FileTreeComponent {
			return sidepane.NewFileTree(entries)
		},
		ParseTOC: func(lines []diff.DiffLine, filename string) ui.TOCComponent {
			toc := sidepane.ParseTOC(lines, filename)
			if toc == nil {
				return nil // collapse typed-nil *TOC into truly nil interface
			}
			return toc
		},
	})
	if err != nil {
		return entryModelResult{}, fmt.Errorf("create model: %w", err)
	}

	// entryModel is what the tea.Program actually runs: NewRootReview for
	// every diff-argument invocation (unchanged from before the browser
	// existed — `q` exits the process, exactly as the Claude Code
	// integration depends on), or a browser-rooted RootModel, with review
	// pushed and popped behind it, when decision routes there (task 23).
	var entryModel tea.Model
	switch decision.screen {
	case routeBrowser:
		root, navCmd := buildRootBrowser(opts, model, km, res, decision.browserScope)
		entryModel = initCmdModel{Model: root, extra: navCmd}
	default:
		entryModel = ui.NewRootReview(model)
	}

	return entryModelResult{
		entryModel:     entryModel,
		programOptions: programOptions,
		tty:            tty,
		gitRoot:        gitRoot,
		workDir:        workDir,
	}, nil
}

// run drives buildEntryModel's composition with a real tea.Program against
// the real terminal: it is the only caller that needs the full ProgramOption
// set (alt screen, mouse tracking, --stdin's tty) and the only place annotation
// output actually reaches disk/stdout for a live invocation. Tests exercise
// buildEntryModel directly and substitute their own ProgramOptions instead
// (see app/e2e_contract_test.go) rather than calling run, since a real
// terminal is not available in a test process.
func run(opts options) (int, error) {
	build, err := buildEntryModel(opts)
	if err != nil {
		return 0, err
	}
	if build.tty != nil {
		defer build.tty.Close()
	}

	p := tea.NewProgram(build.entryModel, build.programOptions...)
	finalModel, err := p.Run()
	if err != nil {
		return 0, fmt.Errorf("TUI error: %w", err)
	}

	// output annotations to stdout or file
	root, ok := unwrapRootModel(finalModel)
	if !ok {
		return 0, nil
	}

	// Before any annotation early-return: the shell wrapper waiting on
	// --cwd-file must learn where the user ended up whether or not they
	// left a comment, so this cannot sit below the "no annotations" exits.
	writeCwdFile(opts, root)

	if root.Discarded() {
		return 0, nil
	}
	output := root.Store().FormatOutput()
	if output == "" {
		return 0, nil
	}

	saveHistory(histReq{opts: opts, annotations: output, gitRoot: build.gitRoot, workDir: build.workDir, files: root.Store().Files()})

	return writeAnnotationOutput(annotationOutputReq{opts: opts, output: output, stdout: os.Stdout})
}

// routeScreen identifies which of RootModel's two top-level screens
// (app/ui/root.go) an invocation should start on.
type routeScreen int

const (
	routeReview routeScreen = iota
	routeBrowser
)

// routeDecision is the pure result of routing one invocation: which screen
// to start on, and — only meaningful when screen is routeBrowser — which
// changed-files scope to preselect the browser with.
type routeDecision struct {
	screen       routeScreen
	browserScope gitstate.Scope
}

// decideRoute implements the task 23 startup routing matrix as a pure
// function over parsed CLI options: no filesystem or git access, no
// tea.Program construction, so the whole matrix is testable without
// starting a terminal program (see app/routing_test.go).
//
//	ydiff                    -> browser, at the current directory
//	ydiff --only=plan.md     -> review, single file (the planning plugin's path)
//	ydiff main / main..feat  -> review, that comparison
//	ydiff --browser main     -> browser, with `branch` scope preselected
//
// --browser always forces the browser screen, even alongside a diff
// argument that would otherwise route to review (options.Browser's own doc
// comment: "force the browser screen even when diff arguments are
// present"). Absent --browser, any argument naming a concrete review target
// routes to review, exactly as it always has for the Claude Code
// integration (hasReviewTarget); a bare invocation with none of those falls
// through to the browser, in ScopeUncommitted.
func decideRoute(opts options) routeDecision {
	if opts.Browser {
		return routeDecision{screen: routeBrowser, browserScope: browserScopeFor(opts)}
	}
	if hasReviewTarget(opts) {
		return routeDecision{screen: routeReview}
	}
	return routeDecision{screen: routeBrowser, browserScope: gitstate.ScopeUncommitted}
}

// hasReviewTarget reports whether opts names a concrete review target:
// a ref/comparison, --only, --stdin, compare mode, or --all-files. Absent
// --browser, any of these routes straight to review.
func hasReviewTarget(opts options) bool {
	return opts.Stdin ||
		opts.compareAbsOld != "" ||
		len(opts.Only) > 0 ||
		opts.AllFiles ||
		opts.ref() != ""
}

// browserScopeFor picks the changed-files scope --browser preselects the
// browser with: ScopeBranch when a ref/comparison was also given
// (`ydiff --browser main`, comparing against that branch's fork point),
// ScopeUncommitted otherwise (`ydiff --browser`, working-tree changes).
func browserScopeFor(opts options) gitstate.Scope {
	if opts.ref() != "" {
		return gitstate.ScopeBranch
	}
	return gitstate.ScopeUncommitted
}

// vcsSetupForRoute wraps setupVCSRenderer with one browser-only exception:
// outside any git repository, setupVCSRenderer normally reports
// errNoVCSRepository (no --only to fall back to standalone file review) —
// exactly right for the review route, but wrong for the browser route, where
// the design requires a bare `ydiff` outside a repository to still open the
// browser with an empty changed pane rather than refusing to start. In that
// one case this substitutes browserFallbackSetup's empty placeholder
// renderer instead of propagating the error; every other error, and every
// review-route error, is returned unchanged.
func vcsSetupForRoute(opts options, decision routeDecision) (vcsSetup, error) {
	setup, err := setupVCSRenderer(opts)
	if err == nil {
		return setup, nil
	}
	if decision.screen == routeBrowser && errors.Is(err, errNoVCSRepository) {
		return browserFallbackSetup(), nil
	}
	return vcsSetup{}, err
}

// browserFallbackSetup returns the vcsSetup used to back the review screen
// pushed behind the browser when the browser itself is standing outside any
// git repository: a FileReader with no files (ChangedFiles returns nil, nil
// harmlessly) rooted at the current directory, replaced the moment the user
// picks a concrete file to review (RootModel.enterReview sets cfg.only and
// reloads).
func browserFallbackSetup() vcsSetup {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return vcsSetup{renderer: diff.NewFileReader(nil, cwd), workDir: cwd}
}

// buildRootBrowser constructs the browser-rooted RootModel and the
// navigation command that must run alongside its own Init (see
// ui.NewRootBrowser's doc comment: the initial directory load command is
// browser.NewNav's responsibility, not RootModel.Init's). The gitstate.Cache
// is backed by a Loader that resolves --base-branch / per-repository
// overrides (opts.resolveBaseBranch) fresh for whichever repository the
// browser currently stands in, since the browser can navigate across
// repository boundaries over its lifetime while a single flat override
// could not.
func buildRootBrowser(opts options, review ui.Model, km *keymap.Keymap, res style.Resolver, scope gitstate.Scope) (ui.RootModel, tea.Cmd) {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	nav, navCmd := browser.NewNav(cwd, false, nil)

	cache := gitstate.NewCache(func(root string, s gitstate.Scope) ([]gitstate.ChangedFile, error) {
		repo := &gitstate.Repo{Root: root}
		if s == gitstate.ScopeBranch {
			return gitstate.BranchScope(repo, opts.resolveBaseBranch(root))
		}
		return gitstate.UncommittedStatus(repo)
	})

	root := ui.NewRootBrowser(nav, km, review, cache, scope, res, opts.ResolvedBrowserWidths())
	return root, navCmd
}

// initCmdModel wraps a tea.Model to run one extra command alongside its own
// Init, without changing what Update/View do. It exists solely to batch
// buildRootBrowser's navCmd (browser.Nav's initial directory load) alongside
// RootModel.Init (which only issues the changed-files pane's own initial
// load) when starting the tea.Program — the composition root's
// responsibility per ui.NewRootBrowser's doc comment. Update's return value
// is whatever the embedded Model's own Update returns (ui.RootModel, not
// this wrapper), so it only exists for the very first Init call.
type initCmdModel struct {
	tea.Model
	extra tea.Cmd
}

func (m initCmdModel) Init() tea.Cmd {
	return tea.Batch(m.Model.Init(), m.extra)
}

// unwrapRootModel extracts the ui.RootModel that finished running, whether
// tea.Program's final value is the plain RootModel (the ordinary case: at
// least one Update ran) or still the initCmdModel wrapper (the edge case
// where the program quit before any Update was processed).
func unwrapRootModel(m tea.Model) (ui.RootModel, bool) {
	switch fm := m.(type) {
	case ui.RootModel:
		return fm, true
	case initCmdModel:
		rm, ok := fm.Model.(ui.RootModel)
		return rm, ok
	default:
		return ui.RootModel{}, false
	}
}

type annotationOutputReq struct {
	opts   options
	output string
	stdout io.Writer
}

func writeAnnotationOutput(r annotationOutputReq) (int, error) {
	code := annotationExitCode(r.opts.ExitCodeOnAnnotations, r.output)
	if r.opts.Output != "" {
		if err := fsutil.AtomicWriteFile(r.opts.Output, []byte(r.output)); err != nil {
			return 0, fmt.Errorf("write output: %w", err)
		}
		return code, nil
	}
	if _, err := fmt.Fprint(r.stdout, r.output); err != nil {
		return 0, fmt.Errorf("write output: %w", err)
	}
	return code, nil
}

func annotationExitCode(enabled bool, output string) int {
	if enabled && output != "" {
		return exitCodeAnnotations
	}
	return 0
}

// reloadApplicable returns false when --stdin is active: the stream has already
// been consumed and cannot be re-read. All other modes support reload.
func reloadApplicable(opts options) bool {
	return !opts.Stdin
}

func sourceEditorPolicy(opts options, workDir string) ui.SourceEditorPolicy {
	switch {
	case opts.Stdin:
		return ui.SourceEditorPolicy{} // unsupported
	case opts.compareAbsNew != "":
		// Always prefer --compare-new in compare mode.
		return ui.SourceEditorPolicy{
			Available: true,
			Root:      filepath.Dir(opts.compareAbsNew),
			ExactPath: opts.compareAbsNew,
		}
	case workDir != "":
		worktreeReview := !opts.Staged && opts.ref() == ""
		return ui.SourceEditorPolicy{
			Available: true,
			Root:      workDir,
			// When reviewing worktree changes, reload after edits and disallow
			// editing annotated files because edits can orphan comments.
			ReloadAfterCleanExit:         worktreeReview,
			DisallowAnnotatedFileEditing: worktreeReview,
		}
	default:
		return ui.SourceEditorPolicy{}
	}
}

// resolveKeysPath returns the effective keybindings file path, falling back
// to defaultKeysPath() when --keys was not set. Extracted from run() to keep
// its cyclomatic complexity under the gocyclo limit after compare-mode
// dispatch was added.
func resolveKeysPath(opts options) string {
	if opts.Keys == "" {
		return defaultKeysPath()
	}
	return opts.Keys
}

// commitsApplicable returns true when the unified info popup can include a
// commit-log section: a VCS-backed log source must be present and the mode
// must be ref-based (no stdin, staged, all-files, or empty ref). Computed
// once in the composition root so the Model does not re-derive from CLI
// flags. --only is fine when combined with a ref in a real repo; the empty
// ref check excludes the standalone --only / FileReader case where the
// commitLogger is nil anyway.
func commitsApplicable(opts options, cl diff.CommitLogger) bool {
	if cl == nil {
		return false
	}
	if opts.Stdin || opts.Staged || opts.AllFiles {
		return false
	}
	return opts.ref() != ""
}

// compactApplicable returns true when the current invocation can shrink the
// VCS diff via the compact toggle. false for stdin (no VCS), all-files (no
// hunks to contextualize), and standalone file review via FileReader (pure
// context-only source with no underlying VCS). All other renderer shapes —
// *Git, with or without Fallback / Include / Exclude wrappers — qualify
// because the wrapper chain delegates FileDiff straight through to a VCS
// that honors contextLines.
func compactApplicable(opts options, r ui.Renderer) bool {
	if opts.Stdin || opts.AllFiles {
		return false
	}
	if _, ok := r.(*diff.FileReader); ok {
		return false
	}
	return true
}

// writeCwdFile records the browser's final directory in opts.CwdFile so a
// shell wrapper can cd there after ydiff exits — the mechanism yazi uses for
// its `y` function. A failure here is deliberately silent: the user's review
// session succeeded, and killing the exit path over an unwritable scratch
// file would lose their annotations for a convenience feature.
func writeCwdFile(opts options, root ui.RootModel) {
	if opts.CwdFile == "" {
		return
	}
	path, ok := root.BrowserPath()
	if !ok {
		return // no browser in this invocation: nothing meaningful to record
	}
	_ = os.WriteFile(opts.CwdFile, []byte(path), 0o600)
}
