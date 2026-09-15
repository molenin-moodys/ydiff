package ui

import (
	"github.com/molenin-moodys/ydiff/app/ui/overlay"
	"github.com/molenin-moodys/ydiff/app/ui/style"
)

// browserThemePreview holds the browser screen's state for an active theme
// selector session — just the resolver to restore on cancel. Unlike the
// review screen's themePreviewSession, there is no renderer, SGR, or
// chroma-highlighted diff to save: the browser screen has none of those, it
// only ever colors its own panes through b.resolver.
type browserThemePreview struct {
	origResolver style.Resolver
}

// openThemeSelector builds the theme list from the browser's ThemeCatalog
// and opens the shared theme-selector popup (overlay.KindThemeSelect), the
// same overlay the review screen uses — it dispatches navigation off raw
// key types rather than an Action namespace, so no browser-specific overlay
// code is needed. A nil catalog (WithThemeCatalog never called) or a
// listing error both leave the browser working exactly as before, mirroring
// openFavorites' fail-open posture.
func (b *browserScreen) openThemeSelector() {
	if b.overlay == nil || b.themes == nil {
		return
	}
	entries, err := b.themes.Entries()
	if err != nil {
		b.hint = "themes: " + err.Error()
		return
	}

	b.themePreview = &browserThemePreview{origResolver: b.resolver}

	items := make([]overlay.ThemeItem, len(entries))
	for i, e := range entries {
		items[i] = overlay.ThemeItem{Name: e.Name, Local: e.Local, AccentColor: e.AccentColor}
	}
	b.overlay.OpenThemeSelect(overlay.ThemeSelectSpec{Items: items, ActiveName: b.activeThemeName})
}

// previewTheme applies name's colors to the browser's resolver as the
// cursor moves over it in the popup, without persisting anything — mirrors
// Model.previewThemeByName.
func (b *browserScreen) previewTheme(name string) {
	if b.themePreview == nil || b.themes == nil {
		return
	}
	spec, ok := b.themes.Resolve(name)
	if !ok {
		return
	}
	b.applyThemeSpec(spec)
}

// confirmTheme applies name, persists it via the catalog, and clears the
// preview session. A persist error surfaces as a status-bar hint rather
// than being silently dropped or blocking the browser — mirrors every other
// favorites/theme error path in this screen.
func (b *browserScreen) confirmTheme(name string) {
	if b.themePreview == nil {
		return
	}
	if spec, ok := b.themes.Resolve(name); ok {
		b.activeThemeName = name
		b.applyThemeSpec(spec)
	}
	if err := b.themes.Persist(name); err != nil {
		b.hint = "themes: " + err.Error()
	}
	b.themePreview = nil
}

// cancelThemeSelect restores the resolver active before the popup opened.
func (b *browserScreen) cancelThemeSelect() {
	if b.themePreview == nil {
		return
	}
	b.resolver = b.themePreview.origResolver
	b.themePreview = nil
}

// applyThemeSpec rebuilds the browser's resolver from spec's colors, unless
// noColors is set — mirroring Model.applyTheme's own --no-colors override,
// so a theme choice never re-enables color once the flag disabled it.
func (b *browserScreen) applyThemeSpec(spec ThemeSpec) {
	if b.noColors {
		b.resolver = style.PlainResolver()
		return
	}
	b.resolver = style.NewResolver(spec.Colors)
}
