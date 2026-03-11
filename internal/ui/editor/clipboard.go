package editor

import (
	"io"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

var writeClipboard = systemWriteClipboard

func copyToClipboardCmd(text string, linewise bool) tea.Cmd {
	if linewise && text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if text == "" {
		return nil
	}
	return func() tea.Msg {
		_ = writeClipboard(text)
		return nil
	}
}

func systemWriteClipboard(text string) error {
	switch runtime.GOOS {
	case "windows":
		return runClipboardCommand(text, "cmd", "/c", "clip")
	case "darwin":
		return runClipboardCommand(text, "pbcopy")
	default:
		for _, cmd := range [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}} {
			if _, err := exec.LookPath(cmd[0]); err == nil {
				return runClipboardCommand(text, cmd[0], cmd[1:]...)
			}
		}
		return nil
	}
}

func runClipboardCommand(text, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return err
	}
	if _, err := io.WriteString(stdin, text); err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return err
	}
	if err := stdin.Close(); err != nil {
		_ = cmd.Wait()
		return err
	}
	return cmd.Wait()
}
