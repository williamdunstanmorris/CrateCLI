// Package browser opens URLs in the user's default browser.
package browser

import (
	"os/exec"
	"runtime"
	"time"
)

// Open opens one URL in the default browser.
func Open(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// OpenAll opens each URL as a new tab, pausing briefly so the browser keeps
// them in order.
func OpenAll(urls []string) error {
	for i, u := range urls {
		if i > 0 {
			time.Sleep(150 * time.Millisecond)
		}
		if err := Open(u); err != nil {
			return err
		}
	}
	return nil
}
