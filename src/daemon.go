package src

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const scanInterval = 2 * time.Second

func pidPath() (string, error) {
	dir, _, err := configPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "dotd.pid"), nil
}

func logPath() (string, error) {
	dir, _, err := configPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "dotd.log"), nil
}

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// runningPID returns the pid in the pid file if that process is alive.
func runningPID() (int, bool) {
	p, err := pidPath()
	if err != nil {
		return 0, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || !processAlive(pid) {
		return 0, false
	}
	return pid, true
}

// Run is the daemon itself: it blocks, organising ~/Downloads until it
// receives SIGINT or SIGTERM.
func Run() error {
	cfg, err := InitConfig()
	if err != nil {
		return err
	}
	root, err := DownloadsDir()
	if err != nil {
		return err
	}
	if err := CreateFileSystem(root, cfg); err != nil {
		return err
	}

	if pid, ok := runningPID(); ok && pid != os.Getpid() {
		return fmt.Errorf("already running (pid %d)", pid)
	}
	pp, err := pidPath()
	if err != nil {
		return err
	}
	if err := os.WriteFile(pp, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return fmt.Errorf("writing pid file: %w", err)
	}
	defer os.Remove(pp)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	log.Printf("dotd started (pid %d), watching %s", os.Getpid(), root)

	t := NewTracker(root, cfg)
	t.Poll()

	ticker := time.NewTicker(scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("dotd stopped")
			return nil
		case <-ticker.C:
			t.Poll()
		}
	}
}

// Start launches the daemon detached from the terminal (no systemd needed).
func Start() error {
	if systemdActive() {
		return errors.New("already running under systemd (see: dotd status)")
	}
	if pid, ok := runningPID(); ok {
		return fmt.Errorf("already running (pid %d)", pid)
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	lp, err := logPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(lp), 0o755); err != nil {
		return err
	}
	logFile, err := os.OpenFile(lp, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()

	cmd := exec.Command(exe, "run")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // survive terminal close
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting daemon: %w", err)
	}
	_ = cmd.Process.Release()

	time.Sleep(500 * time.Millisecond)
	if pid, ok := runningPID(); ok {
		fmt.Printf("dotd started (pid %d), log: %s\n", pid, lp)
		return nil
	}
	return fmt.Errorf("daemon exited immediately, check %s", lp)
}

// Stop stops the daemon, whether it was started by systemd or by `dotd start`.
// If installed, it will still start again on the next boot.
func Stop() error {
	if systemdActive() {
		if err := systemctl("stop", unitName); err != nil {
			return err
		}
		fmt.Println("dotd stopped (systemd)")
		return nil
	}

	pid, ok := runningPID()
	if !ok {
		fmt.Println("dotd is not running")
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("signalling pid %d: %w", pid, err)
	}
	for i := 0; i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
		if !processAlive(pid) {
			fmt.Println("dotd stopped")
			return nil
		}
	}
	return fmt.Errorf("pid %d did not exit within 5s", pid)
}

// Status prints whether the daemon is running and whether it starts on boot.
func Status() {
	if pid, ok := runningPID(); ok {
		fmt.Printf("running (pid %d)\n", pid)
	} else {
		fmt.Println("not running")
	}
	if systemdAvailable() {
		if systemctl("is-enabled", "--quiet", unitName) == nil {
			fmt.Println("autostart: enabled (systemd user service)")
		} else {
			fmt.Println("autostart: not installed (run: dotd install)")
		}
	}
}
