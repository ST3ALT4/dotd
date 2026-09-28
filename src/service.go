package src

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

const unitName = "dotd.service"

func systemdAvailable() bool {
	_, err := exec.LookPath("systemctl")
	return err == nil
}

// systemctl runs `systemctl --user <args...>`.
func systemctl(args ...string) error {
	cmd := exec.Command("systemctl", append([]string{"--user"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, msg)
	}
	return nil
}

func systemdActive() bool {
	return systemdAvailable() && systemctl("is-active", "--quiet", unitName) == nil
}

func unitPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", unitName), nil
}

// Install registers dotd as a systemd user service that starts on boot,
// and starts it now.
func Install() error {
	if !systemdAvailable() {
		return errors.New("systemctl not found: autostart needs a systemd-based Linux")
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	if strings.Contains(exe, "go-build") || strings.HasPrefix(exe, os.TempDir()) {
		return errors.New("this looks like a `go run` binary; build a permanent one first:\n" +
			"  go build -o ~/.local/bin/dotd . && ~/.local/bin/dotd install")
	}

	// A manually started daemon would clash with the service on the pid file.
	if !systemdActive() {
		if _, ok := runningPID(); ok {
			if err := Stop(); err != nil {
				return err
			}
		}
	}

	unit := fmt.Sprintf(`[Unit]
Description=dotd - Downloads organizer

[Service]
Type=simple
ExecStart=%q run
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
`, exe)

	up, err := unitPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(up), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(up, []byte(unit), 0o644); err != nil {
		return err
	}

	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	if err := systemctl("enable", "--now", unitName); err != nil {
		return err
	}
	fmt.Println("installed and started:", up)

	// User services normally start at login. Lingering makes them start at boot.
	if u, err := user.Current(); err == nil {
		if out, err := exec.Command("loginctl", "enable-linger", u.Username).CombinedOutput(); err != nil {
			fmt.Printf("note: could not enable lingering (%s)\n"+
				"      run `sudo loginctl enable-linger %s` to start at boot before you log in\n",
				strings.TrimSpace(string(out)), u.Username)
		}
	}
	return nil
}

// Uninstall stops the service, disables autostart and removes the unit file.
func Uninstall() error {
	if !systemdAvailable() {
		return errors.New("systemctl not found")
	}
	_ = systemctl("disable", "--now", unitName)

	up, err := unitPath()
	if err != nil {
		return err
	}
	if err := os.Remove(up); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = systemctl("daemon-reload")
	fmt.Println("autostart removed")
	return nil
}
