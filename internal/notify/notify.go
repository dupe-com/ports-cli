// Package notify sends best-effort desktop notifications. Failures are
// returned but callers generally ignore them — a missing notifier should
// never break the tool.
package notify

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Send shows a desktop notification with the given title and body.
func Send(title, body string) error {
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf("display notification %q with title %q", sanitize(body), sanitize(title))
		return exec.Command("osascript", "-e", script).Run()
	case "linux":
		if _, err := exec.LookPath("notify-send"); err != nil {
			return fmt.Errorf("notify-send not found: %w", err)
		}
		return exec.Command("notify-send", "--app-name=ports", title, body).Run()
	case "windows":
		// PowerShell toast via the BurntToast-free fallback: a msg box is
		// too intrusive, so use the WinRT toast API inline.
		ps := fmt.Sprintf(`[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null; `+
			`$x = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02); `+
			`$t = $x.GetElementsByTagName('text'); $t.Item(0).AppendChild($x.CreateTextNode(%q)) | Out-Null; $t.Item(1).AppendChild($x.CreateTextNode(%q)) | Out-Null; `+
			`[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('ports-cli').Show([Windows.UI.Notifications.ToastNotification]::new($x))`,
			sanitize(title), sanitize(body))
		return exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps).Run()
	default:
		return fmt.Errorf("notifications unsupported on %s", runtime.GOOS)
	}
}

// sanitize strips characters that could escape the quoting context of the
// platform notifier invocations above.
func sanitize(s string) string {
	s = strings.ReplaceAll(s, `"`, "'")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
