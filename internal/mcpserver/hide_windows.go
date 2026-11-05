package mcpserver

import (
	"os/exec"
	"syscall"
)

// hideWindow stops the GUI app flashing a console window when it runs the memory CLI.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
