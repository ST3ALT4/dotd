# agents.md — dotd

> [!NOTE]
> This file documents the project for AI coding agents. It covers architecture, key types/functions, conventions, and gotchas to help an agent make correct changes quickly.

## Project overview

**dotd** is a small Go daemon that watches `~/Downloads` and automatically sorts newly-added files into sub-folders based on their extension (e.g. `.pdf` → `docs/pdf`). It can be run in the foreground, launched as a detached background process, or managed as a systemd user service.

- **Language:** Go 1.27  
- **Module:** `github.com/ST3ALT4/dotd`  
- **No external dependencies** (stdlib only)

---

## Repository layout

```
dotd/
├── main.go          # CLI entry-point; parses the sub-command and delegates to src/
└── src/
    ├── conf.go      # Config loading, validation, and filesystem setup
    ├── track.go     # File-watching logic (Tracker, Poll/Scan, move)
    ├── daemon.go    # Lifecycle: Run, Start, Stop, Status
    └── service.go   # systemd user-service integration (Install, Uninstall)
```

---

## Key types and functions

### `src/conf.go`

| Symbol | Purpose |
|--------|---------|
| `FileConfig` | One rule: an `Extension` mapped to a `Location` (relative path inside `~/Downloads`) |
| `Config` | Top-level config struct; holds `[]FileConfig` |
| `DefaultConfig() *Config` | Returns built-in rules for 12 common extensions |
| `InitConfig() (*Config, error)` | Loads `~/.config/dotd/conf.json`; creates it with defaults if absent |
| `(c *Config) normalize() error` | Lowercases extensions, ensures leading `.`, rejects duplicates and path-escape attempts |
| `(c *Config) LocationFor(ext string) (string, bool)` | Returns the destination sub-folder for a given extension |
| `CreateFileSystem(root string, cfg *Config) error` | Pre-creates all destination folders under `root` |
| `configPath() (dir, file string, err error)` | Returns `~/.config/dotd/` and `~/.config/dotd/conf.json` |

### `src/track.go`

| Symbol | Purpose |
|--------|---------|
| `partialExts` | Set of extensions browsers use for in-progress downloads; these are **never** moved |
| `Tracker` | Holds the watch root, config, a `pending` map (path → `fileState`), and the last-seen dir mtime |
| `fileState` | `{size int64, modNano int64}` — snapshot used to detect when a file stops changing |
| `NewTracker(root, cfg) *Tracker` | Constructor |
| `(t *Tracker) Poll()` | Cheap entry-point called on every tick: stats the dir and skips `Scan` if mtime is unchanged and nothing is pending |
| `(t *Tracker) Scan()` | Full scan: moves any file whose `fileState` is identical between two consecutive calls |
| `(t *Tracker) move(src, dir string) (string, error)` | Calls `uniquePath` then `os.Rename` |
| `uniquePath(dir, name string) string` | Returns a non-colliding destination path, appending ` (1)`, ` (2)` etc. as needed |
| `DownloadsDir() (string, error)` | Returns `~/Downloads` |

### `src/daemon.go`

| Symbol | Purpose |
|--------|---------|
| `Run() error` | **Foreground daemon.** Acquires the flock lock (preventing duplicates), sets up the rotating log (unless under systemd), initialises config and filesystem, then loops on a 2-second ticker calling `t.Poll()` until SIGINT/SIGTERM |
| `Start() error` | Checks for a running daemon via `runningPID()`, then spawns `dotd run` as a detached child (new session via `Setsid`). Child stdout/stderr → `/dev/null`; the child manages its own log |
| `Stop() error` | Stops the process: via `systemctl stop` if systemd-managed, otherwise sends SIGTERM to the PID read from the lock file and polls for exit |
| `Status()` | Prints whether a daemon is running (via `runningPID()`) and whether autostart is enabled |
| `acquireLock() (*os.File, error)` | Opens `/tmp/dotd-<uid>.lock`, takes a non-blocking exclusive flock, writes the PID. Caller must keep the file open for the lock to remain held |
| `runningPID() (int, bool)` | Tries a non-blocking exclusive flock on the lock file. If it fails, the lock is held; reads the PID from the file and verifies the process is alive |
| `lockFilePath() string` | Returns `/tmp/dotd-<uid>.lock` — no runtime state in `~/.config` |
| `rotatingWriter` | `sync.Mutex`-guarded `io.WriteCloser` that rotates the log at `maxLogSize` (1 MB). On rotation: renames current log to `dotd.log.1` (deleting any older backup), opens a fresh log |
| `newRotatingWriter(path, maxSize)` | Constructor for `rotatingWriter`; reads the current file size so the limit is respected across restarts |
| `underSystemd() bool` | Returns `true` when `JOURNAL_STREAM` env var is set (systemd sets this for every service unit); used to skip file logging when journald handles it |
| `logPath() (string, error)` | Returns `~/.config/dotd/dotd.log` |
| `processAlive(pid int) bool` | Sends signal 0 to check liveness |
| `maxLogSize` | Constant `1 << 20` (1 MB) — rotating threshold |
| `scanInterval` | Constant `2s` — how often `Poll` is called |

### `src/service.go`

| Symbol | Purpose |
|--------|---------|
| `unitName` | `"dotd.service"` |
| `Install() error` | Writes a systemd user unit file to `~/.config/systemd/user/dotd.service`, runs `daemon-reload` + `enable --now`, and attempts `loginctl enable-linger` for boot-time start |
| `Uninstall() error` | Disables and removes the unit file |
| `systemdAvailable() bool` | Checks that `systemctl` is on PATH |
| `systemdActive() bool` | Checks `systemctl --user is-active dotd.service` |
| `systemctl(args ...string) error` | Thin wrapper: runs `systemctl --user <args>` and returns a combined error+output on failure |
| `unitPath() (string, error)` | Returns `~/.config/systemd/user/dotd.service` |

---

## Data flow

```
main.go
  └─ src.Run()
       ├─ InitConfig()          # load/create ~/.config/dotd/conf.json
       ├─ DownloadsDir()        # resolve ~/Downloads
       ├─ CreateFileSystem()    # mkdir all destination folders
       └─ Tracker.Poll() × ∞   # every 2 s
            └─ Tracker.Scan()  # when dir mtime changed or pending files exist
                 └─ move()     # os.Rename after two stable consecutive scans
```

---

## Config file

Path: `~/.config/dotd/conf.json`

```json
{
  "files": [
    { "extension": ".pdf", "location": "docs/pdf" },
    { "extension": ".png", "location": "images/png" }
  ]
}
```

- Extensions are normalised to lowercase with a leading `.`.
- `location` must be a **relative** path; absolute paths and `..` escapes are rejected by `normalize()`.
- Duplicate extensions are also rejected.

---

## Conventions and rules

1. **Two-scan stability check.** A file is only moved after its `size` and `modNano` are identical across two consecutive `Scan` calls. Never move a file on the first time it is seen.
2. **Partial-download guard.** Any file whose extension is in `partialExts` is skipped entirely, every scan.
3. **Top-level only.** Only regular files directly inside `~/Downloads` are touched. Sub-directories are not recursed into.
4. **No hidden files.** Files whose name starts with `.` are skipped.
5. **Collision-safe rename.** `uniquePath` ensures a destination path is free before `os.Rename`; it never overwrites.
6. **systemd wins.** If `systemdActive()` is true, `Stop` delegates to `systemctl stop` and `Start` refuses to spawn a second process.
7. **Lingering.** `Install` tries `loginctl enable-linger` so the user service starts at boot even before the user logs in; failure is a soft warning, not an error.

---

## Adding a new file type

Edit `~/.config/dotd/conf.json` at runtime, or add an entry to `DefaultConfig()` in [`src/conf.go`](file:///home/ani/projects/dotd/src/conf.go) to include it out of the box.

---

## Known / watch-out items

- **Lock file in `/tmp`** — the flock lock at `/tmp/dotd-<uid>.lock` is cleaned up automatically when the daemon exits (the OS releases flocks when the fd is closed). If the system reboots without a clean shutdown, the stale lock file does no harm — the flock will not be held and `runningPID()` will correctly return `false`.
- **Log files in `~/.config/dotd/`** — `dotd.log` (active) and `dotd.log.1` (latest backup) are the only files written there now. The old `dotd.pid` no longer exists.
- **systemd path skips file logging** — when `JOURNAL_STREAM` is set (i.e. started via `dotd install` / systemd), the rotating writer is not set up; use `journalctl --user -u dotd` instead.
- **`scanInterval` is a constant** (`2s`). If you make it configurable, surface it in `Config` and re-read it in `Run`.
- **`Install` guards against `go run` binaries** by checking for `go-build` in the executable path, since an ephemeral path would make the unit file useless.
