// Package browser walks the local filesystem and reports directory listings for a
// yazi-style Miller-column browser. It knows the filesystem and a cursor over it —
// nothing about git, diffs, or rendering. Package ui decides how to present what
// browser reports and wires it to git state from package gitstate.
package browser
