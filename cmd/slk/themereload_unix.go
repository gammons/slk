//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// watchThemeReloadSignal catches SIGUSR1, which a palette generator (or a
// script) sends after rewriting a custom theme file:
// `pkill -USR1 -x slk`. Call it before the theme files are first read:
// the channel keeps a signal that arrives before the returned start runs,
// so a file rewritten during startup is still reloaded. start runs reload
// for every signal.
func watchThemeReloadSignal() (start func(reload func())) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGUSR1)
	return func(reload func()) {
		go func() {
			for range sig {
				reload()
			}
		}()
	}
}
