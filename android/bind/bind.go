// Package bind is the module gomobile builds the Android app's Go library
// from. It's separate so the main module needn't depend on gomobile, which
// needs a newer Go than tsuzuki itself.
package bind

import (
	_ "github.com/EmoFa/tsuzuki/mobile"
	_ "golang.org/x/mobile/bind"
)
