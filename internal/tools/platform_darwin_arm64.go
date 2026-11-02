//go:build darwin && arm64

package tools

import "embed"

//go:embed all:assets/darwin_arm64
var embeddedAssets embed.FS

// Embedded returns the asset tree for this build target.
func Embedded() (embed.FS, string) { return embeddedAssets, "darwin_arm64" }
