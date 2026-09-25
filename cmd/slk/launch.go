package main

import (
	"os/exec"
	"runtime"
)

// launchOS starts the platform's default handler for target (a URL or
// file path): open (macOS), rundll32 (Windows), xdg-open (Linux).
func launchOS(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}
