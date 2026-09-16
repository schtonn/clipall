<p align="center">
  <img src="https://img.shields.io/github/v/release/schtonn/clipall?style=flat-square&color=blue" alt="Release">
  <img src="https://img.shields.io/github/actions/workflow/status/schtonn/clipall/ci.yml?style=flat-square&label=CI" alt="CI">
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Windows-lightgrey?style=flat-square" alt="Platform">
  <img src="https://img.shields.io/github/license/schtonn/clipall?style=flat-square" alt="License">
</p>

<h1 align="center">clipall</h1>

<p align="center">
  Cross-platform clipboard sync over <a href="https://tailscale.com">Tailscale</a>.<br>
  Copy text or images on Mac, paste on Windows. And vice versa. Instantly.
</p>

---

## How It Works

```
  macOS                          Windows
┌──────────┐    Tailscale     ┌──────────┐
│ clipall   │◄── TCP:9876 ──►│ clipall   │
│           │   (WireGuard)   │           │
│ clipboard │  text + image   │ clipboard │
│  watch    │     (PNG)       │  watch    │
└──────────┘                  └──────────┘
```

Each device runs a lightweight daemon that watches the local clipboard. When you copy text or an image, it's sent to all peers over your existing Tailscale network. The peer writes it to its local clipboard. Done. Image data is transferred in PNG format.

- **No cloud.** Traffic stays on your Tailscale network, encrypted end-to-end by WireGuard.
- **No account.** No sign-up, no server, no subscription.
- **~5MB binary.** Single executable, zero config required.

## Quick Start

### Install

**macOS:**

```bash
curl -fsSL https://raw.githubusercontent.com/schtonn/clipall/main/install.sh | bash
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/schtonn/clipall/main/install.ps1 | iex
```

Or download binaries manually from [Releases](https://github.com/schtonn/clipall/releases).

### Run

Run the same command on each device:

```bash
clipall
```

On first run, clipall asks whether it should start automatically in the
background, discovers all peers from `tailscale status --json`, and asks whether
to use all of them. The accepted peers are saved to the default config file.
Use `--peers host1:9876,host2:9876` whenever you want to override discovery.

That's it. Copy on one machine, paste on the other.

## Configuration

### CLI Flags

```
--peers    Comma-separated peer addresses (host:port)
--listen-host  Address to listen on (default: tailscale; use * for all interfaces)
--listen   Port to listen on (default: 9876)
--config   Path to config file
--files    Enable on-demand file copy/paste (default: true; use --files=false to disable)
--install-autostart     Start clipall automatically at login
--uninstall-autostart   Remove automatic startup
```

### Config File (Optional)

Place a YAML file at `~/.config/clipall/config.yaml` (macOS) or `%APPDATA%\clipall\config.yaml` (Windows):

```yaml
peers:
  - hostname: windows
    port: 9876
  - hostname: macbook
    port: 9876

listen:
  host: tailscale
  port: 9876
```

Then just run `clipall` with no arguments.

### Experimental On-Demand File Paste

Enable the prototype on both devices:

```bash
clipall --files --peers <hostname>:9876
```

macOS and Windows can be file sources and paste destinations. Copying sends
metadata only. The file contents remain on the source until Finder or Windows
Explorer requests the paste, then clipall downloads them to a private staging
directory. macOS uses AppKit's lazy pasteboard provider and does not require
Accessibility or Input Monitoring permission. Windows currently detects
`Ctrl+V`; context-menu Paste is not intercepted on Windows.

Folders are supported up to 4,096 total entries. Symbolic links and special
files are rejected. Both devices must run a version that supports `--files`.

### Start Automatically at Login

After installing clipall, register it with the same peer arguments you normally use:

```bash
clipall --install-autostart --peers <hostname>:9876
```

If your peers are already in the default config file, no extra arguments are needed:

```bash
clipall --install-autostart
```

This creates a user LaunchAgent on macOS or a current-user scheduled task on
Windows. No administrator privileges are required. Both restart clipall after
an unexpected exit. Windows retries after one minute, prevents duplicate task
instances, and writes a rotating log to
`%LOCALAPPDATA%\clipall\clipall.log`. Reinstalling autostart replaces the old
task and its arguments. To remove it:

```bash
clipall --uninstall-autostart
```

## Build from Source

Requires [Go](https://go.dev/dl/) 1.24+.

```bash
git clone https://github.com/schtonn/clipall.git
cd clipall
go build -o clipall .
```

Cross-compile for Windows from Mac:

```bash
GOOS=windows GOARCH=amd64 go build -o clipall.exe .
```

## Architecture

| Component | File | Purpose |
|-----------|------|---------|
| Wire Protocol | `protocol.go` | Versioned event metadata + payload |
| Loop Prevention | `loop.go` | Event-ID ring buffer + targeted echo suppression |
| Clipboard | `clipboard.go` | Watch/Read/Write text and images (PNG) via native APIs |
| Networking | `peer.go` | TCP connections with auto-reconnect |
| On-demand files | `file_sync.go`, `clipboard_files_*.go` | Lazy file offers, transfer, and native clipboard integration |
| Orchestrator | `node.go` | Event loop tying everything together |
| Autostart | `autostart_*.go` | LaunchAgent / Windows user startup registration |
| Config | `config.go` | YAML + CLI flag parsing |

### Loop Prevention

When device A syncs to device B, writing to B's clipboard triggers B's watcher and could create an infinite loop. Clipall identifies an event by its **content hash, source node, and timestamp**. This lets a user copy identical content again without that new event being mistaken for an old duplicate. A short-lived, content-specific echo marker suppresses only the clipboard notification caused by a remote write; unrelated copies are never blocked by a global cooldown.

This metadata is carried by wire protocol v2. The decoder accepts v1 messages during upgrades, but all peers should be upgraded because older binaries cannot decode v2 messages.

## Prerequisites

- [Tailscale](https://tailscale.com/download) installed and running on all devices
- Devices must be on the same Tailnet (verify with `tailscale ping <hostname>`)
- Port 9876 (default) must be reachable between devices

By default, clipall listens only on a local Tailscale address. If you intentionally
use another private network, pass `--listen-host <ip>`; `--listen-host '*'` listens
on all interfaces and should only be used behind a trusted firewall.

## Roadmap

- [x] Image clipboard sync (PNG)
- [ ] System tray icon with connection status
- [x] Auto-start with restart-on-failure (LaunchAgent / Windows Task Scheduler)
- [ ] Clipboard history

## License

MIT
