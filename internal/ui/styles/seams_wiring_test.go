package styles

import "os"

// How seams_test.go hands a theme directory to the loader. The scenarios
// must not change when the loader's signature does; this file should.
func loadThemesFrom(dir string) {
	LoadCustomThemes(os.DirFS(dir))
}
