<p align="center">
  <img src="https://img.shields.io/github/v/release/MrDuan-DLy/clipall?style=flat-square&color=blue" alt="Release">
  <img src="https://img.shields.io/github/actions/workflow/status/MrDuan-DLy/clipall/ci.yml?style=flat-square&label=CI" alt="CI">
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Windows-lightgrey?style=flat-square" alt="Platform">
  <img src="https://img.shields.io/github/license/MrDuan-DLy/clipall?style=flat-square" alt="License">
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

**macOS / Linux:**

```bash
curl -fsSL https://raw.githubusercontent.com/MrDuan-DLy/clipall/main/install.sh | bash
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/MrDuan-DLy/clipall/main/install.ps1 | iex
```

Or download binaries manually from [Releases](https://github.com/MrDuan-DLy/clipall/releases).

### Run

On your Mac:

```bash
clipall --peers <windows-hostname>:9876
```

On your Windows machine:

```powershell
clipall --peers <mac-hostname>:9876
```

Replace `<windows-hostname>` and `<mac-hostname>` with the Tailscale hostnames of your devices (check with `tailscale status`).

That's it. Copy on one machine, paste on the other.

## Configuration

### CLI Flags

```
--peers    Comma-separated peer addresses (host:port)
--listen   Port to listen on (default: 9876)
--config   Path to config file
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
  port: 9876
```

Then just run `clipall` with no arguments.

## Build from Source

Requires [Go](https://go.dev/dl/) 1.22+.

```bash
git clone https://github.com/MrDuan-DLy/clipall.git
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
| Orchestrator | `node.go` | Event loop tying everything together |
| Config | `config.go` | YAML + CLI flag parsing |

### Loop Prevention

When device A syncs to device B, writing to B's clipboard triggers B's watcher and could create an infinite loop. Clipall identifies an event by its **content hash, source node, and timestamp**. This lets a user copy identical content again without that new event being mistaken for an old duplicate. A short-lived, content-specific echo marker suppresses only the clipboard notification caused by a remote write; unrelated copies are never blocked by a global cooldown.

This metadata is carried by wire protocol v2. The decoder accepts v1 messages during upgrades, but all peers should be upgraded because older binaries cannot decode v2 messages.

## Prerequisites

- [Tailscale](https://tailscale.com/download) installed and running on all devices
- Devices must be on the same Tailnet (verify with `tailscale ping <hostname>`)
- Port 9876 (default) must be reachable between devices

## Roadmap

- [x] Image clipboard sync (PNG)
- [ ] System tray icon with connection status
- [ ] Auto-start (launchd / Task Scheduler)
- [ ] Clipboard history

## License

MIT
