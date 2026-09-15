package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- --base-branch ---------------------------------------------------------

func TestParseArgs_BaseBranch_Flag(t *testing.T) {
	opts, err := parseArgs(append(noConfigArgs(t), "--base-branch=origin/main"))
	require.NoError(t, err)
	assert.Equal(t, "origin/main", opts.BaseBranch)
}

func TestParseArgs_BaseBranch_Env(t *testing.T) {
	t.Setenv("YDIFF_BASE_BRANCH", "origin/develop")
	opts, err := parseArgs(noConfigArgs(t))
	require.NoError(t, err)
	assert.Equal(t, "origin/develop", opts.BaseBranch)
}

func TestParseArgs_BaseBranch_ConfigFile(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(cfgPath, []byte("[Application Options]\nbase-branch = origin/release\n"), 0o600))

	opts, err := parseArgs([]string{"--config", cfgPath})
	require.NoError(t, err)
	assert.Equal(t, "origin/release", opts.BaseBranch)
}

func TestParseArgs_BaseBranch_FlagOverridesEnvAndConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(cfgPath, []byte("[Application Options]\nbase-branch = origin/release\n"), 0o600))
	t.Setenv("YDIFF_BASE_BRANCH", "origin/develop")

	opts, err := parseArgs([]string{"--config", cfgPath, "--base-branch=origin/main"})
	require.NoError(t, err)
	assert.Equal(t, "origin/main", opts.BaseBranch, "flag must win over both env and config")
}

// TestParseArgs_BaseBranch_ConfigWinsOverEnv documents this codebase's
// established config-vs-env precedence (see TestParseArgs_ConfigFile and
// friends): once the config file sets a value, go-flags marks it as already
// set and skips applying the environment variable for it. Only the CLI flag
// can override a config-file value; env only fills in when neither flag nor
// config set the option.
func TestParseArgs_BaseBranch_ConfigWinsOverEnv(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(cfgPath, []byte("[Application Options]\nbase-branch = origin/release\n"), 0o600))
	t.Setenv("YDIFF_BASE_BRANCH", "origin/develop")

	opts, err := parseArgs([]string{"--config", cfgPath})
	require.NoError(t, err)
	assert.Equal(t, "origin/release", opts.BaseBranch)
}

// --- per-repository base-branch --------------------------------------------

func TestParseArgs_BaseBranchRepos_ConfigFile(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(cfgPath, []byte(
		"[Application Options]\n"+
			"base-branch-repo = /repos/mono:origin/develop\n"+
			"base-branch-repo = /repos/other:origin/main\n",
	), 0o600))

	opts, err := parseArgs([]string{"--config", cfgPath})
	require.NoError(t, err)
	assert.Equal(t, "origin/develop", opts.BaseBranchRepos["/repos/mono"])
	assert.Equal(t, "origin/main", opts.BaseBranchRepos["/repos/other"])
}

func TestResolveBaseBranch_PerRepositoryOverride(t *testing.T) {
	opts, err := parseArgs(noConfigArgs(t))
	require.NoError(t, err)
	opts.BaseBranchRepos = map[string]string{"/repos/mono": "origin/develop"}

	assert.Equal(t, "origin/develop", opts.resolveBaseBranch("/repos/mono"))
	assert.Equal(t, "", opts.resolveBaseBranch("/repos/unknown"))
	assert.Equal(t, "", opts.resolveBaseBranch(""))
}

func TestResolveBaseBranch_FlagOverridesPerRepository(t *testing.T) {
	opts, err := parseArgs(append(noConfigArgs(t), "--base-branch=origin/main"))
	require.NoError(t, err)
	opts.BaseBranchRepos = map[string]string{"/repos/mono": "origin/develop"}

	assert.Equal(t, "origin/main", opts.resolveBaseBranch("/repos/mono"), "--base-branch always wins")
}

// --- --browser ---------------------------------------------------------------

func TestParseArgs_Browser_Default(t *testing.T) {
	opts, err := parseArgs(noConfigArgs(t))
	require.NoError(t, err)
	assert.False(t, opts.Browser)
}

func TestParseArgs_Browser_Flag(t *testing.T) {
	opts, err := parseArgs(append(noConfigArgs(t), "--browser"))
	require.NoError(t, err)
	assert.True(t, opts.Browser)
}

func TestParseArgs_Browser_Env(t *testing.T) {
	t.Setenv("YDIFF_BROWSER", "true")
	opts, err := parseArgs(noConfigArgs(t))
	require.NoError(t, err)
	assert.True(t, opts.Browser)
}

func TestParseArgs_Browser_ConfigFile(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(cfgPath, []byte("[Application Options]\nbrowser = true\n"), 0o600))

	opts, err := parseArgs([]string{"--config", cfgPath})
	require.NoError(t, err)
	assert.True(t, opts.Browser)
}

func TestParseArgs_Browser_FlagOverridesEnvAndConfig(t *testing.T) {
	// bool flags in this CLI library are switches (no "--browser=false"
	// form), so the precedence demonstration is: config and env both leave
	// it false, and the bare flag forces it on regardless.
	cfgPath := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(cfgPath, []byte("[Application Options]\nbrowser = false\n"), 0o600))
	t.Setenv("YDIFF_BROWSER", "false")

	opts, err := parseArgs([]string{"--config", cfgPath, "--browser"})
	require.NoError(t, err)
	assert.True(t, opts.Browser, "flag must win over both env and config")
}

// --- --browser-widths ---------------------------------------------------------

func TestParseArgs_BrowserWidths_Default(t *testing.T) {
	opts, err := parseArgs(noConfigArgs(t))
	require.NoError(t, err)
	assert.Equal(t, "15,35,50", opts.BrowserWidths)
	assert.Equal(t, [3]int{15, 35, 50}, opts.ResolvedBrowserWidths())
}

func TestParseArgs_BrowserWidths_Flag(t *testing.T) {
	opts, err := parseArgs(append(noConfigArgs(t), "--browser-widths=20,30,50"))
	require.NoError(t, err)
	assert.Equal(t, [3]int{20, 30, 50}, opts.ResolvedBrowserWidths())
}

func TestParseArgs_BrowserWidths_Env(t *testing.T) {
	t.Setenv("YDIFF_BROWSER_WIDTHS", "10,40,50")
	opts, err := parseArgs(noConfigArgs(t))
	require.NoError(t, err)
	assert.Equal(t, [3]int{10, 40, 50}, opts.ResolvedBrowserWidths())
}

func TestParseArgs_BrowserWidths_ConfigFile(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(cfgPath, []byte("[Application Options]\nbrowser-widths = 25,25,50\n"), 0o600))

	opts, err := parseArgs([]string{"--config", cfgPath})
	require.NoError(t, err)
	assert.Equal(t, [3]int{25, 25, 50}, opts.ResolvedBrowserWidths())
}

func TestParseArgs_BrowserWidths_FlagOverridesEnvAndConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(cfgPath, []byte("[Application Options]\nbrowser-widths = 25,25,50\n"), 0o600))
	t.Setenv("YDIFF_BROWSER_WIDTHS", "10,40,50")

	opts, err := parseArgs([]string{"--config", cfgPath, "--browser-widths=1,1,1"})
	require.NoError(t, err)
	assert.Equal(t, [3]int{1, 1, 1}, opts.ResolvedBrowserWidths(), "flag must win over both env and config")
}

func TestParseArgs_BrowserWidths_NeedNotSumTo100(t *testing.T) {
	opts, err := parseArgs(append(noConfigArgs(t), "--browser-widths=1,2,3"))
	require.NoError(t, err, "proportions are normalised at render time, not required to sum to 100")
	assert.Equal(t, [3]int{1, 2, 3}, opts.ResolvedBrowserWidths())
}

func TestParseArgs_BrowserWidths_RejectsNonNumeric(t *testing.T) {
	_, err := parseArgs(append(noConfigArgs(t), "--browser-widths=a,b,c"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--browser-widths")
	assert.Contains(t, err.Error(), "not a number")
}

func TestParseArgs_BrowserWidths_RejectsNonPositive(t *testing.T) {
	_, err := parseArgs(append(noConfigArgs(t), "--browser-widths=15,0,50"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--browser-widths")
	assert.Contains(t, err.Error(), "must be positive")

	_, err = parseArgs(append(noConfigArgs(t), "--browser-widths=15,-5,50"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be positive")
}

func TestParseArgs_BrowserWidths_RejectsWrongCount(t *testing.T) {
	_, err := parseArgs(append(noConfigArgs(t), "--browser-widths=15,85"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly 3")

	_, err = parseArgs(append(noConfigArgs(t), "--browser-widths=15,35,25,25"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly 3")
}

// --- parseBrowserWidths unit tests ------------------------------------------

func TestParseBrowserWidths(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    [3]int
		wantErr string
	}{
		{name: "default", in: "15,35,50", want: [3]int{15, 35, 50}},
		{name: "not summing to 100 is fine", in: "1,2,3", want: [3]int{1, 2, 3}},
		{name: "whitespace tolerated", in: " 15 , 35 , 50 ", want: [3]int{15, 35, 50}},
		{name: "non-numeric", in: "a,35,50", wantErr: "not a number"},
		{name: "zero rejected", in: "0,35,50", wantErr: "must be positive"},
		{name: "negative rejected", in: "15,-1,50", wantErr: "must be positive"},
		{name: "too few", in: "15,35", wantErr: "exactly 3"},
		{name: "too many", in: "15,35,25,25", wantErr: "exactly 3"},
		{name: "empty", in: "", wantErr: "exactly 3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseBrowserWidths(tt.in)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
