package apply

import "os/exec"

// DefaultReload reloads sing-box via systemd, falling back to restart if the
// service does not support live reload.
func DefaultReload() error {
	if err := exec.Command("systemctl", "reload", "sing-box").Run(); err == nil {
		return nil
	}
	return exec.Command("systemctl", "restart", "sing-box").Run()
}
