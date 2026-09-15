package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jessevdk/go-flags"
)

type options struct {
	Refs struct {
		Base    string `positional-arg-name:"base" description:"git ref to diff against (default: uncommitted changes)"`
		Against string `positional-arg-name:"against" description:"second git ref for two-ref diff (e.g. ydiff main feature)"`
	} `positional-args:"yes"`

	Staged                bool     `long:"staged" ini-name:"staged" env:"YDIFF_STAGED" description:"show staged changes"`
	Untracked             bool     `long:"untracked" ini-name:"untracked" env:"YDIFF_UNTRACKED" description:"show untracked files in the tree"`
	TreeWidth             int      `long:"tree-width" ini-name:"tree-width" env:"YDIFF_TREE_WIDTH" default:"2" description:"file tree panel width in units (1-10, default 2 of 10)"`
	TabWidth              int      `long:"tab-width" ini-name:"tab-width" env:"YDIFF_TAB_WIDTH" default:"4" description:"number of spaces per tab character"`
	NoColors              bool     `long:"no-colors" ini-name:"no-colors" env:"YDIFF_NO_COLORS" description:"disable all colors including syntax highlighting"`
	NoStatusBar           bool     `long:"no-status-bar" ini-name:"no-status-bar" env:"YDIFF_NO_STATUS_BAR" description:"hide the status bar"`
	NoConfirmDiscard      bool     `long:"no-confirm-discard" ini-name:"no-confirm-discard" env:"YDIFF_NO_CONFIRM_DISCARD" description:"skip confirmation prompt when discarding annotations with Q"`
	NoConfirmReload       bool     `long:"no-confirm-reload" ini-name:"no-confirm-reload" env:"YDIFF_NO_CONFIRM_RELOAD" description:"skip confirmation prompt when dropping annotations on reload with R"`
	NoMouse               bool     `long:"no-mouse" ini-name:"no-mouse" env:"YDIFF_NO_MOUSE" description:"disable mouse support (scroll wheel, click)"`
	Wrap                  bool     `long:"wrap" ini-name:"wrap" env:"YDIFF_WRAP" description:"enable line wrapping in diff view"`
	WrapIndent            int      `long:"wrap-indent" ini-name:"wrap-indent" env:"YDIFF_WRAP_INDENT" default:"0" description:"indent wrap continuation rows by N columns so they hang under the first row's content (helps when reviewing markdown lists where unindented continuation can be misread as a new bullet)"`
	Collapsed             bool     `long:"collapsed" ini-name:"collapsed" env:"YDIFF_COLLAPSED" description:"start in collapsed diff mode"`
	Compact               bool     `long:"compact" ini-name:"compact" env:"YDIFF_COMPACT" description:"start in compact diff mode (small context around changes)"`
	CompactContext        int      `long:"compact-context" ini-name:"compact-context" env:"YDIFF_COMPACT_CONTEXT" default:"5" description:"number of context lines around changes when in compact mode"`
	CrossFileHunks        bool     `long:"cross-file-hunks" ini-name:"cross-file-hunks" env:"YDIFF_CROSS_FILE_HUNKS" description:"allow [ and ] to jump across file boundaries"`
	LineNumbers           bool     `long:"line-numbers" ini-name:"line-numbers" env:"YDIFF_LINE_NUMBERS" description:"show line numbers in diff gutter"`
	Blame                 bool     `long:"blame" ini-name:"blame" env:"YDIFF_BLAME" description:"show blame gutter"`
	WordDiff              bool     `long:"word-diff" ini-name:"word-diff" env:"YDIFF_WORD_DIFF" description:"highlight intra-line word-level changes in paired add/remove lines"`
	AnnotationMarker      string   `long:"annotation-marker" ini-name:"annotation-marker" env:"YDIFF_ANNOTATION_MARKER" default:"💬" description:"prefix shown before annotation lines"`
	ExitCodeOnAnnotations bool     `long:"exit-code-on-annotations" ini-name:"exit-code-on-annotations" env:"YDIFF_EXIT_CODE_ON_ANNOTATIONS" description:"exit 10 when annotations are produced"`
	ChromaStyle           string   `long:"chroma-style" ini-name:"chroma-style" env:"YDIFF_CHROMA_STYLE" default:"catppuccin-macchiato" description:"chroma style for syntax highlighting"`
	AllFiles              bool     `long:"all-files" short:"A" no-ini:"true" description:"browse all tracked files, not just diffs (git only)"`
	CompareOld            string   `long:"compare-old" no-ini:"true" description:"compare mode: old file path (use with --compare-new)"`
	CompareNew            string   `long:"compare-new" no-ini:"true" description:"compare mode: new file path (use with --compare-old)"`
	Stdin                 bool     `long:"stdin" no-ini:"true" description:"review stdin as a scratch buffer"`
	StdinName             string   `long:"stdin-name" no-ini:"true" description:"synthetic file name for stdin content"`
	Annotations           string   `long:"annotations" no-ini:"true" description:"preload annotations from a markdown file written by -o (round-trip)"`
	Description           string   `long:"description" no-ini:"true" description:"prose context shown in the info popup (markdown; use shell multiline quoting or --description-file for multiple lines)"`
	DescriptionFile       string   `long:"description-file" no-ini:"true" description:"read the info-popup description from this file (markdown)"`
	Exclude               []string `long:"exclude" short:"X" ini-name:"exclude" env:"YDIFF_EXCLUDE" env-delim:"," description:"exclude files matching prefix (may be repeated)"`
	Include               []string `long:"include" short:"I" ini-name:"include" env:"YDIFF_INCLUDE" env-delim:"," description:"include only files matching prefix (may be repeated)"`
	Only                  []string `long:"only" short:"F" no-ini:"true" description:"show only these files (may be repeated)"`
	HistoryDir            string   `long:"history-dir" ini-name:"history-dir" env:"YDIFF_HISTORY_DIR" description:"directory for review history auto-saves"`
	Output                string   `long:"output" short:"o" env:"YDIFF_OUTPUT" no-ini:"true" description:"write annotations to file instead of stdout"`
	Keys                  string   `long:"keys" env:"YDIFF_KEYS" no-ini:"true" description:"path to keybindings file"`
	DumpKeys              bool     `long:"dump-keys" no-ini:"true" description:"print effective keybindings to stdout and exit"`
	Theme                 string   `long:"theme" ini-name:"theme" env:"YDIFF_THEME" description:"load theme from themes directory"`
	AutoThemeDark         string   `long:"auto-theme-dark" ini-name:"auto-theme-dark" env:"YDIFF_AUTO_THEME_DARK" default:"ydiff" description:"theme to use for dark terminal backgrounds when --theme=auto"`
	AutoThemeLight        string   `long:"auto-theme-light" ini-name:"auto-theme-light" env:"YDIFF_AUTO_THEME_LIGHT" default:"catppuccin-latte" description:"theme to use for light terminal backgrounds when --theme=auto"`
	DumpTheme             bool     `long:"dump-theme" no-ini:"true" description:"print currently resolved colors as theme file and exit"`
	ListThemes            bool     `long:"list-themes" no-ini:"true" description:"print available theme names and exit"`
	InitThemes            bool     `long:"init-themes" no-ini:"true" description:"write bundled theme files to themes dir and exit"`
	InitAllThemes         bool     `long:"init-all-themes" no-ini:"true" description:"write all gallery themes (bundled + community) to themes dir and exit"`
	InstallTheme          []string `long:"install-theme" no-ini:"true" description:"install theme(s) from gallery or local file path and exit"`
	Config                string   `long:"config" env:"YDIFF_CONFIG" no-ini:"true" description:"path to config file"`
	DumpConfig            bool     `long:"dump-config" no-ini:"true" description:"print default config to stdout and exit"`
	Version               bool     `short:"V" long:"version" no-ini:"true" description:"show version info"`

	BaseBranch string `long:"base-branch" ini-name:"base-branch" env:"YDIFF_BASE_BRANCH" description:"default branch used to find the fork point for branch-scope base resolution (e.g. origin/main); overrides auto-detection"`
	// BaseBranchRepos is a per-repository override for BaseBranch, keyed by
	// the repository's absolute root path. It exists only in the config
	// file (and, incidentally, as a repeatable CLI flag via go-flags' map
	// support) so a monorepo whose base is always e.g. origin/develop can be
	// pinned once without exporting an env var or flag for every invocation.
	// --base-branch (flag, env, or the plain config default) always takes
	// precedence over this map — see resolveBaseBranch.
	BaseBranchRepos map[string]string `long:"base-branch-repo" ini-name:"base-branch-repo" description:"per-repository base branch override: repo-root-path:branch (repeatable)"`
	Browser         bool              `long:"browser" ini-name:"browser" env:"YDIFF_BROWSER" description:"force the browser screen even when diff arguments are present"`
	BrowserWidths   string            `long:"browser-widths" ini-name:"browser-widths" env:"YDIFF_BROWSER_WIDTHS" default:"15,35,50" description:"parent,current,changed column proportions for the browser screen (normalised, need not sum to 100)"`

	Colors struct {
		Accent       string `long:"color-accent"      ini-name:"color-accent"      env:"YDIFF_COLOR_ACCENT"      default:"#D5895F" description:"active pane borders and directory names"`
		Border       string `long:"color-border"      ini-name:"color-border"      env:"YDIFF_COLOR_BORDER"      default:"#585858" description:"inactive pane borders"`
		Normal       string `long:"color-normal"      ini-name:"color-normal"      env:"YDIFF_COLOR_NORMAL"      default:"#d0d0d0" description:"file entries and context lines"`
		Muted        string `long:"color-muted"       ini-name:"color-muted"       env:"YDIFF_COLOR_MUTED"       default:"#585858" description:"line numbers and status bar"`
		SelectedFg   string `long:"color-selected-fg" ini-name:"color-selected-fg" env:"YDIFF_COLOR_SELECTED_FG" default:"#ffffaf" description:"selected file text color"`
		SelectedBg   string `long:"color-selected-bg" ini-name:"color-selected-bg" env:"YDIFF_COLOR_SELECTED_BG" default:"#D5895F" description:"selected file background color"`
		Annotation   string `long:"color-annotation"  ini-name:"color-annotation"  env:"YDIFF_COLOR_ANNOTATION"  default:"#ffd700" description:"annotation text and markers"`
		CursorFg     string `long:"color-cursor-fg"   ini-name:"color-cursor-fg"   env:"YDIFF_COLOR_CURSOR_FG"   default:"#bbbb44" description:"diff cursor indicator color"`
		CursorBg     string `long:"color-cursor-bg"   ini-name:"color-cursor-bg"   env:"YDIFF_COLOR_CURSOR_BG"   description:"diff cursor indicator background"`
		AddFg        string `long:"color-add-fg"      ini-name:"color-add-fg"      env:"YDIFF_COLOR_ADD_FG"      default:"#87d787" description:"added line text color"`
		AddBg        string `long:"color-add-bg"      ini-name:"color-add-bg"      env:"YDIFF_COLOR_ADD_BG"      default:"#123800" description:"added line background color"`
		RemoveFg     string `long:"color-remove-fg"   ini-name:"color-remove-fg"   env:"YDIFF_COLOR_REMOVE_FG"   default:"#ff8787" description:"removed line text color"`
		RemoveBg     string `long:"color-remove-bg"   ini-name:"color-remove-bg"   env:"YDIFF_COLOR_REMOVE_BG"   default:"#4D1100" description:"removed line background color"`
		WordAddBg    string `long:"color-word-add-bg"    ini-name:"color-word-add-bg"    env:"YDIFF_COLOR_WORD_ADD_BG"    description:"intra-line word-diff add background (auto-derived if empty)"`
		WordRemoveBg string `long:"color-word-remove-bg" ini-name:"color-word-remove-bg" env:"YDIFF_COLOR_WORD_REMOVE_BG" description:"intra-line word-diff remove background (auto-derived if empty)"`
		ModifyFg     string `long:"color-modify-fg"      ini-name:"color-modify-fg"      env:"YDIFF_COLOR_MODIFY_FG"      default:"#f5c542" description:"modified line text color (collapsed mode)"`
		ModifyBg     string `long:"color-modify-bg"   ini-name:"color-modify-bg"   env:"YDIFF_COLOR_MODIFY_BG"   default:"#3D2E00" description:"modified line background color (collapsed mode)"`
		TreeBg       string `long:"color-tree-bg"     ini-name:"color-tree-bg"     env:"YDIFF_COLOR_TREE_BG"     description:"file tree pane background"`
		DiffBg       string `long:"color-diff-bg"     ini-name:"color-diff-bg"     env:"YDIFF_COLOR_DIFF_BG"     description:"diff pane background"`
		StatusFg     string `long:"color-status-fg"   ini-name:"color-status-fg"   env:"YDIFF_COLOR_STATUS_FG"   default:"#202020" description:"status bar foreground"`
		StatusBg     string `long:"color-status-bg"   ini-name:"color-status-bg"   env:"YDIFF_COLOR_STATUS_BG"   default:"#C5794F" description:"status bar background"`
		SearchFg     string `long:"color-search-fg"   ini-name:"color-search-fg"   env:"YDIFF_COLOR_SEARCH_FG"   default:"#1a1a1a" description:"search match foreground"`
		SearchBg     string `long:"color-search-bg"   ini-name:"color-search-bg"   env:"YDIFF_COLOR_SEARCH_BG"   default:"#4a4a00" description:"search match background"`
	} `group:"color options"`

	compareAbsOld string
	compareAbsNew string
	browserWidths [3]int
}

// ref returns the combined ref string from positional args.
// two refs are joined with ".." to form a range (e.g. "main..feature").
func (o options) ref() string {
	if o.Refs.Against != "" {
		return o.Refs.Base + ".." + o.Refs.Against
	}
	return o.Refs.Base
}

// startupUntracked reports whether --untracked should activate.
// disabled in two-ref mode (both `a b` and `a..b` forms) because untracked
// files are working-tree state, not part of a historical diff between refs.
func (o options) startupUntracked() bool {
	if !o.Untracked {
		return false
	}
	if o.Refs.Against != "" || strings.Contains(o.Refs.Base, "..") {
		return false
	}
	return true
}

// parseArgs parses CLI arguments with config file support.
// config file is loaded first, then CLI args override.
// precedence: CLI flags > env vars > config file > built-in defaults.
func parseArgs(args []string) (options, error) {
	var opts options
	p := flags.NewParser(&opts, flags.Default)
	p.Usage = "[OPTIONS]"

	// determine config path from args before full parsing
	configPath := resolveFlagPath(args, "config", "YDIFF_CONFIG", defaultConfigPath)

	// load config file before parsing CLI args (CLI overrides config)
	iniParser := flags.NewIniParser(p)
	loadConfigFile(iniParser, configPath)

	if _, err := p.ParseArgs(args); err != nil {
		return options{}, fmt.Errorf("parse args: %w", err)
	}

	if opts.Staged && (opts.Refs.Against != "" || strings.Contains(opts.Refs.Base, "..")) {
		return options{}, errors.New("--staged cannot be used with two-ref diff")
	}

	if opts.AllFiles {
		if opts.Refs.Base != "" || opts.Refs.Against != "" {
			return options{}, errors.New("--all-files cannot be used with refs")
		}
		if opts.Staged {
			return options{}, errors.New("--all-files cannot be used with --staged")
		}
		if len(opts.Only) > 0 {
			return options{}, errors.New("--all-files cannot be used with --only")
		}
	}

	if len(opts.Include) > 0 && len(opts.Only) > 0 {
		return options{}, errors.New("--include cannot be used with --only")
	}

	if opts.CompactContext <= 0 {
		return options{}, errors.New("--compact-context must be >= 1")
	}

	if strings.ContainsAny(opts.AnnotationMarker, "\n\r\t") {
		return options{}, errors.New("--annotation-marker cannot contain control characters")
	}

	if opts.Description != "" && opts.DescriptionFile != "" {
		return options{}, errors.New("--description and --description-file are mutually exclusive")
	}

	if err := validateStdinFlags(opts); err != nil {
		return options{}, err
	}

	absOld, absNew, err := validateCompareFlag(opts)
	if err != nil {
		return options{}, err
	}
	opts.compareAbsOld = absOld
	opts.compareAbsNew = absNew

	widths, err := parseBrowserWidths(opts.BrowserWidths)
	if err != nil {
		return options{}, err
	}
	opts.browserWidths = widths

	return opts, nil
}

// resolveBaseBranch returns the base branch to use for branch-scope base
// resolution when the current repository's root is repoRoot. --base-branch
// (set via flag, env, or the plain config-file default) always wins; only
// when it is empty does a per-repository BaseBranchRepos entry for repoRoot
// apply. Returns "" when neither is set, leaving auto-detection to the
// caller.
func (o options) resolveBaseBranch(repoRoot string) string {
	if o.BaseBranch != "" {
		return o.BaseBranch
	}
	if repoRoot == "" {
		return ""
	}
	return o.BaseBranchRepos[repoRoot]
}

// ResolvedBrowserWidths returns the parsed, normalised parent/current/changed
// column proportions for the browser screen, as validated by parseArgs.
func (o options) ResolvedBrowserWidths() [3]int {
	return o.browserWidths
}

// parseBrowserWidths parses and validates the --browser-widths value: exactly
// three comma-separated positive integers, e.g. "15,35,50". They are treated
// as proportions and normalised by the browser view (app/ui), not required to
// sum to 100 — "1,2,3" is exactly as valid as "15,35,50".
func parseBrowserWidths(s string) ([3]int, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 3 {
		return [3]int{}, fmt.Errorf("--browser-widths must have exactly 3 comma-separated values (parent,current,changed), got %d: %q", len(parts), s)
	}

	var widths [3]int
	for i, part := range parts {
		part = strings.TrimSpace(part)
		n, err := strconv.Atoi(part)
		if err != nil {
			return [3]int{}, fmt.Errorf("--browser-widths value %q is not a number: %q", part, s)
		}
		if n <= 0 {
			return [3]int{}, fmt.Errorf("--browser-widths value %q must be positive: %q", part, s)
		}
		widths[i] = n
	}

	return widths, nil
}

// dumpConfig writes the current config with defaults to the given writer.
func dumpConfig(args []string, w io.Writer) {
	var opts options
	p := flags.NewParser(&opts, flags.Default)
	iniParser := flags.NewIniParser(p)
	configPath := resolveFlagPath(args, "config", "YDIFF_CONFIG", defaultConfigPath)
	loadConfigFile(iniParser, configPath)
	_, _ = p.ParseArgs(args)
	iniParser.Write(w, flags.IniIncludeDefaults|flags.IniCommentDefaults|flags.IniIncludeComments)
}

// loadConfigFile attempts to parse a config file, logging a warning on parse errors.
// silently ignores missing files or empty paths.
func loadConfigFile(iniParser *flags.IniParser, configPath string) {
	if configPath == "" {
		return
	}
	err := iniParser.ParseFile(configPath)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return // file access error (permission denied, etc.)
	}
	fmt.Fprintf(os.Stderr, "warning: config %s: %v\n", configPath, err)
}

// resolveFlagPath determines a file path from CLI args, env var, or default location.
// it checks args for --flag value and --flag=value forms, falls back to envVar, then defaultFn.
func resolveFlagPath(args []string, flag, envVar string, defaultFn func() string) string {
	longFlag := "--" + flag
	for i, arg := range args {
		if arg == longFlag && i+1 < len(args) {
			return args[i+1]
		}
		if after, ok := strings.CutPrefix(arg, longFlag+"="); ok {
			return after
		}
	}
	if p := os.Getenv(envVar); p != "" {
		return p
	}
	return defaultFn()
}

// defaultConfigPath returns ~/.config/ydiff/config.
func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "ydiff", "config")
}

// defaultKeysPath returns ~/.config/ydiff/keybindings.
func defaultKeysPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "ydiff", "keybindings")
}
