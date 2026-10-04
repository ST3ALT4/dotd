package src

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	scanInterval = 2 * time.Second
	maxLogSize   = 1 << 20 // 1 MB
)

// ── log rotation ──────────────────────────────────────────────────────────────

// rotatingWriter is an io.WriteCloser that caps the log file at maxSize bytes.
// When the limit is reached the current file is renamed to "<path>.1" (any
// older backup at that path is deleted first) and a fresh file is opened.
// All methods are safe for concurrent use.
type rotatingWriter struct {
	mu      sync.Mutex
	path    string
	maxSize int64
	file    *os.File
	size    int64
}

// newRotatingWriter opens (or creates) the log file for appending and seeds
// the current byte count so an existing large file triggers rotation immediately.
func newRotatingWriter(path string, maxSize int64) (*rotatingWriter, error) {
	f, sz, err := openAppend(path)
	if err != nil {
		return nil, err
	}
	return &rotatingWriter{path: path, maxSize: maxSize, file: f, size: sz}, nil
}

// NewRotatingWriter is the exported constructor for rotatingWriter.
// It returns an io.WriteCloser so callers (including tests) are not coupled
// to the concrete type.
func NewRotatingWriter(path string, maxSize int64) (interface {
	io.Writer
	io.Closer
}, error) {
	return newRotatingWriter(path, maxSize)
}

// openAppend opens path in append mode (creating it if absent) and returns
// the file handle along with its current size for rotation accounting.
func openAppend(path string) (*os.File, int64, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

// Write appends p to the log file. If the write would exceed maxSize, a
// best-effort rotation is attempted first; on failure, writing continues to the
// existing file rather than dropping the message.
func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size+int64(len(p)) > w.maxSize {
		// Best-effort rotate; if it fails keep writing to the current file.
		_ = w.rotate()
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// rotate closes the current file, moves it to "<path>.1" (overwriting any
// previous backup), and opens a fresh log file. Called under w.mu.
func (w *rotatingWriter) rotate() error {
	_ = w.file.Close()
	backup := w.path + ".1"
	_ = os.Remove(backup)
	_ = os.Rename(w.path, backup)
	f, sz, err := openAppend(w.path)
	if err != nil {
		return err
	}
	w.file = f
	w.size = sz
	return nil
}

// Close flushes and closes the underlying file. Safe for concurrent use.
func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}

// ── process lock (replaces pid file) ─────────────────────────────────────────

// lockFilePath returns a per-user lock file in the system temp directory.
// Keeping it in /tmp means nothing runtime-related is stored in ~/.config.
func lockFilePath() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("dotd-%d.lock", os.Getuid()))
}

// acquireLock opens (or creates) the lock file and takes an exclusive,
// non-blocking flock on it. The caller must keep the returned *os.File open
// for the lifetime of the process — closing it releases the lock automatically.
// Returns an error immediately if another process already holds the lock.
func acquireLock() (*os.File, error) {
	f, err := os.OpenFile(lockFilePath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errors.New("dotd is already running")
		}
		return nil, fmt.Errorf("acquiring lock: %w", err)
	}
	// Write our PID into the lock file so Stop / Status can signal us without
	// a separate pid file.
	_ = f.Truncate(0)
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	return f, nil
}

// runningPID returns the PID stored in the lock file when another process is
// actively holding the flock.  Returns (0, false) when no daemon is running.
func runningPID() (int, bool) {
	f, err := os.OpenFile(lockFilePath(), os.O_RDWR, 0o600)
	if err != nil {
		return 0, false // lock file absent → nothing running
	}
	defer f.Close()

	// If we can grab a non-blocking exclusive lock, the file is unlocked
	// (no daemon running). Release it and report.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return 0, false
	}

	// Lock is held by another process — read the PID it wrote.
	b, err := io.ReadAll(f)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || !processAlive(pid) {
		return 0, false
	}
	return pid, true
}

// ── helpers ───────────────────────────────────────────────────────────────────

// logPath returns the path of the daemon's log file inside the config directory.
func logPath() (string, error) {
	dir, _, err := configPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "dotd.log"), nil
}

// processAlive reports whether a process with the given PID exists and is
// reachable by the current user. Uses kill(pid, 0), which sends no signal but
// returns an error if the process doesn't exist or isn't accessible.
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// underSystemd reports whether the process was launched by systemd.
// systemd sets JOURNAL_STREAM in the environment for every service unit.
func underSystemd() bool {
	return os.Getenv("JOURNAL_STREAM") != ""
}

// ── lifecycle ─────────────────────────────────────────────────────────────────

// Run is the daemon itself: it blocks, organising ~/Downloads until it
// receives SIGINT or SIGTERM.
func Run() error {
	// Acquire a flock-based lock. This prevents a second dotd from running
	// regardless of how it was started. The lock is released when the file
	// descriptor is closed on exit — no stale pid file to clean up.
	lock, err := acquireLock()
	if err != nil {
		return err
	}
	defer lock.Close()

	// When not managed by systemd (which handles log collection itself),
	// redirect the standard logger to a size-capped, self-rotating file.
	if !underSystemd() {
		lp, err := logPath()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(lp), 0o755); err != nil {
			return err
		}
		rw, err := newRotatingWriter(lp, maxLogSize)
		if err != nil {
			return fmt.Errorf("opening log: %w", err)
		}
		defer rw.Close()
		log.SetOutput(rw)
	}

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

	// The child (dotd run) manages its own rotating log; discard its
	// stdout/stderr so nothing leaks to the terminal after detach.
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer devNull.Close()

	cmd := exec.Command(exe, "run")
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // survive terminal close
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting daemon: %w", err)
	}
	_ = cmd.Process.Release()

	// Give the child a moment to acquire the lock and write its PID.
	time.Sleep(500 * time.Millisecond)
	if pid, ok := runningPID(); ok {
		lp, _ := logPath()
		fmt.Printf("dotd started (pid %d), log: %s\n", pid, lp)
		return nil
	}
	lp, _ := logPath()
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
