package overlay

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/molenin-moodys/ydiff/app/keymap"
	"github.com/molenin-moodys/ydiff/app/ui/style"
)

func favSpec(paths ...string) FavoritesSpec {
	return FavoritesSpec{Items: paths}
}

func favRenderCtx() RenderCtx {
	return RenderCtx{Width: 80, Height: 30, Resolver: style.PlainResolver()}
}

func TestFavoritesOverlay_RenderEmpty(t *testing.T) {
	mgr := NewManager()
	mgr.OpenFavorites(favSpec())
	result := mgr.favorites.render(favRenderCtx(), mgr)
	assert.Contains(t, result, "no favorites yet")
	assert.Contains(t, result, "favorites (0)")
}

func TestFavoritesOverlay_RenderWithItems(t *testing.T) {
	mgr := NewManager()
	mgr.OpenFavorites(favSpec("/repo/one", "/repo/two"))
	result := mgr.favorites.render(favRenderCtx(), mgr)
	assert.Contains(t, result, "favorites (2)")
	assert.Contains(t, result, "/repo/one")
	assert.Contains(t, result, "/repo/two")
}

func TestFavoritesOverlay_RenderCursorHighlight(t *testing.T) {
	mgr := NewManager()
	mgr.OpenFavorites(favSpec("/repo/one", "/repo/two"))
	result := mgr.favorites.render(favRenderCtx(), mgr)
	assert.Contains(t, result, "> ")
}

func TestFavoritesOverlay_RenderFooterHints(t *testing.T) {
	mgr := NewManager()
	mgr.OpenFavorites(favSpec("/repo/one"))
	result := mgr.favorites.render(favRenderCtx(), mgr)
	assert.Contains(t, result, "enter")
	assert.Contains(t, result, "delete")
	assert.Contains(t, result, "esc")
}

func TestFavoritesOverlay_HandleKey_EnterChoosesCurrent(t *testing.T) {
	f := &favoritesOverlay{}
	f.open(favSpec("/repo/one", "/repo/two"))
	f.moveCursorBy(1)

	out := f.handleKey(tea.KeyMsg{Type: tea.KeyEnter}, "")
	require.Equal(t, OutcomeFavoriteChosen, out.Kind)
	assert.Equal(t, "/repo/two", out.FavoritePath)
}

func TestFavoritesOverlay_HandleKey_EnterEmptyListCloses(t *testing.T) {
	f := &favoritesOverlay{}
	f.open(favSpec())

	out := f.handleKey(tea.KeyMsg{Type: tea.KeyEnter}, "")
	assert.Equal(t, OutcomeClosed, out.Kind)
}

func TestFavoritesOverlay_HandleKey_DeleteRequestsRemoval(t *testing.T) {
	f := &favoritesOverlay{}
	f.open(favSpec("/repo/one", "/repo/two"))

	out := f.handleKey(tea.KeyMsg{Type: tea.KeyDelete}, "")
	require.Equal(t, OutcomeFavoriteDeleteRequested, out.Kind)
	assert.Equal(t, "/repo/one", out.FavoritePath)
}

func TestFavoritesOverlay_HandleKey_DeleteEmptyListIsNoOp(t *testing.T) {
	f := &favoritesOverlay{}
	f.open(favSpec())

	out := f.handleKey(tea.KeyMsg{Type: tea.KeyDelete}, "")
	assert.Equal(t, OutcomeNone, out.Kind)
}

func TestFavoritesOverlay_HandleKey_EscCloses(t *testing.T) {
	f := &favoritesOverlay{}
	f.open(favSpec("/repo/one"))

	out := f.handleKey(tea.KeyMsg{Type: tea.KeyEsc}, "")
	assert.Equal(t, OutcomeClosed, out.Kind)
}

func TestFavoritesOverlay_HandleKey_DismissActionCloses(t *testing.T) {
	f := &favoritesOverlay{}
	f.open(favSpec("/repo/one"))

	out := f.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}, keymap.ActionBrowserDismiss)
	assert.Equal(t, OutcomeClosed, out.Kind)
}

func TestFavoritesOverlay_HandleKey_NavigateDownAndUp(t *testing.T) {
	f := &favoritesOverlay{}
	f.open(favSpec("/repo/one", "/repo/two", "/repo/three"))

	f.handleKey(tea.KeyMsg{}, keymap.ActionBrowserDown)
	assert.Equal(t, 1, f.cursor)

	f.handleKey(tea.KeyMsg{}, keymap.ActionBrowserDown)
	assert.Equal(t, 2, f.cursor)

	f.handleKey(tea.KeyMsg{}, keymap.ActionBrowserUp)
	assert.Equal(t, 1, f.cursor)
}

func TestFavoritesOverlay_NavigateBoundsClamp(t *testing.T) {
	f := &favoritesOverlay{}
	f.open(favSpec("/repo/one", "/repo/two"))

	f.handleKey(tea.KeyMsg{}, keymap.ActionBrowserUp) // already at 0
	assert.Equal(t, 0, f.cursor)

	f.handleKey(tea.KeyMsg{}, keymap.ActionBrowserDown)
	f.handleKey(tea.KeyMsg{}, keymap.ActionBrowserDown) // already at last
	assert.Equal(t, 1, f.cursor)
}

func TestFavoritesOverlay_Update_ClampsCursorWhenListShrinks(t *testing.T) {
	f := &favoritesOverlay{}
	f.open(favSpec("/repo/one", "/repo/two", "/repo/three"))
	f.moveCursorBy(2) // cursor on the last item

	f.update(favSpec("/repo/two")) // "one" and "three" removed by the caller
	assert.Equal(t, 0, f.cursor)
	assert.Equal(t, []string{"/repo/two"}, f.items)
}

func TestFavoritesOverlay_Update_EmptyListResetsCursor(t *testing.T) {
	f := &favoritesOverlay{}
	f.open(favSpec("/repo/one"))
	f.update(favSpec())
	assert.Equal(t, 0, f.cursor)
	assert.Empty(t, f.items)
}

func TestFavoritesOverlay_HandleMouse_WheelMovesCursor(t *testing.T) {
	mgr := NewManager()
	mgr.OpenFavorites(favSpec("/repo/one", "/repo/two", "/repo/three"))
	mgr.favorites.render(favRenderCtx(), mgr) // populate height

	out := mgr.favorites.handleMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	assert.Equal(t, OutcomeNone, out.Kind)
	assert.Equal(t, 1, mgr.favorites.cursor)
}

func TestFavoritesOverlay_HandleMouse_ClickOutsideSwallowed(t *testing.T) {
	f := &favoritesOverlay{}
	f.open(favSpec("/repo/one"))
	f.render(favRenderCtx(), NewManager())

	out := f.handleLeftClick(0, 2) // inside the border column
	assert.Equal(t, OutcomeNone, out.Kind)
}

func TestFavoritesOverlay_HandleMouse_ClickSelectsAndChooses(t *testing.T) {
	f := &favoritesOverlay{}
	f.open(favSpec("/repo/one", "/repo/two"))
	f.render(favRenderCtx(), NewManager())

	out := f.handleLeftClick(2, 3) // second content row: index 1
	require.Equal(t, OutcomeFavoriteChosen, out.Kind)
	assert.Equal(t, "/repo/two", out.FavoritePath)
}

func TestFavoritesOverlay_ComposeOnBase(t *testing.T) {
	mgr := NewManager()
	mgr.OpenFavorites(favSpec("/repo/one"))
	out := mgr.Compose("base", favRenderCtx())
	assert.Contains(t, out, "/repo/one")
}

func TestFavoritesOverlay_OpenResetsState(t *testing.T) {
	mgr := NewManager()
	mgr.OpenFavorites(favSpec("/a", "/b", "/c"))
	mgr.favorites.moveCursorBy(2)
	mgr.OpenFavorites(favSpec("/x"))
	assert.Equal(t, 0, mgr.favorites.cursor)
	assert.Equal(t, 0, mgr.favorites.offset)
}

func TestManager_HandleKey_FavoriteChosenClosesOverlay(t *testing.T) {
	mgr := NewManager()
	mgr.OpenFavorites(favSpec("/repo/one"))

	out := mgr.HandleKey(tea.KeyMsg{Type: tea.KeyEnter}, "")
	require.Equal(t, OutcomeFavoriteChosen, out.Kind)
	assert.False(t, mgr.Active(), "choosing a favorite must close the popup")
}

func TestManager_HandleKey_DeleteRequestedKeepsOverlayOpen(t *testing.T) {
	mgr := NewManager()
	mgr.OpenFavorites(favSpec("/repo/one", "/repo/two"))

	out := mgr.HandleKey(tea.KeyMsg{Type: tea.KeyDelete}, "")
	require.Equal(t, OutcomeFavoriteDeleteRequested, out.Kind)
	assert.True(t, mgr.Active(), "a delete request must not close the popup — the caller updates it in place")
}

func TestManager_UpdateFavorites_NoOpWhenNotActive(t *testing.T) {
	mgr := NewManager()
	assert.NotPanics(t, func() {
		mgr.UpdateFavorites(favSpec("/repo/one"))
	})
}

func TestManager_UpdateFavorites_ReplacesItemsInPlace(t *testing.T) {
	mgr := NewManager()
	mgr.OpenFavorites(favSpec("/repo/one", "/repo/two"))

	mgr.UpdateFavorites(favSpec("/repo/two"))

	assert.Equal(t, []string{"/repo/two"}, mgr.favorites.items)
	assert.True(t, mgr.Active(), "UpdateFavorites must not close the popup")
}
