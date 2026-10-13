package main

import (
	"path/filepath"
	"strings"
)

// ensureExt appends ext when the chosen file name has no extension.
//
// The Win32 common save dialog used by github.com/lxn/walk never sets
// lpstrDefExt, so Windows does not add the filter's extension automatically.
// Without this the user gets an extension-less file whenever they type a custom
// name (e.g. "报告" instead of "报告.csv").
func ensureExt(path, ext string) string {
	if path == "" {
		return path
	}
	switch filepath.Ext(path) {
	case "":
		return path + ext
	case ".":
		return strings.TrimSuffix(path, ".") + ext
	default:
		return path
	}
}
