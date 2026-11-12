//go:build windows

package main

// watchThemeReloadSignal does nothing on Windows, which has no SIGUSR1:
// custom themes are read at startup only.
func watchThemeReloadSignal() (start func(reload func())) {
	return func(func()) {}
}
