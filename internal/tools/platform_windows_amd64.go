//go:build windows && amd64

package tools

import "embed"

//go:embed all:assets/windows_amd64
var embeddedAssets embed.FS

// Embedded returns the asset tree for this build target.
func Embedded() (embed.FS, string) { return embeddedAssets, "windows_amd64" }
