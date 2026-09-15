package main

import (
	"fmt"

	"github.com/molenin-moodys/ydiff/app/ui"
)

// configStore implements ui.BrowserWidthsPersister by patching values into the
// INI config file at path, reusing the same key-agnostic patchConfigKey the
// theme catalog uses to persist the selected theme.
type configStore struct {
	path string
}

// compile-time assertion that *configStore satisfies ui.BrowserWidthsPersister.
var _ ui.BrowserWidthsPersister = (*configStore)(nil)

// PersistBrowserWidths writes widths as "browser-widths = a,b,c" into the
// config file at cs.path. an empty path is a silent no-op, mirroring
// themeCatalog.Persist's behavior for an unset config path.
func (cs *configStore) PersistBrowserWidths(widths [3]int) error {
	if cs.path == "" {
		return nil
	}
	value := fmt.Sprintf("%d,%d,%d", widths[0], widths[1], widths[2])
	return patchConfigKey(cs.path, "browser-widths", value)
}
