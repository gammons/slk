# Installation

Grab a prebuilt binary from the [latest release](https://github.com/gammons/slk/releases/latest), or use one of the methods below.

The shell snippets resolve the latest version automatically.

## Linux

### Debian / Ubuntu

```bash
VERSION=$(curl -fsSL https://api.github.com/repos/gammons/slk/releases/latest | grep -oE '"tag_name": *"v[^"]+"' | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | sed 's/^v//')
curl -fsSLO "https://github.com/gammons/slk/releases/latest/download/slk_${VERSION}_linux_amd64.deb"
sudo dpkg -i "slk_${VERSION}_linux_amd64.deb"
```

### Fedora / RHEL

```bash
VERSION=$(curl -fsSL https://api.github.com/repos/gammons/slk/releases/latest | grep -oE '"tag_name": *"v[^"]+"' | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | sed 's/^v//')
sudo rpm -i "https://github.com/gammons/slk/releases/latest/download/slk_${VERSION}_linux_amd64.rpm"
```

### Alpine

```bash
VERSION=$(curl -fsSL https://api.github.com/repos/gammons/slk/releases/latest | grep -oE '"tag_name": *"v[^"]+"' | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | sed 's/^v//')
curl -fsSLO "https://github.com/gammons/slk/releases/latest/download/slk_${VERSION}_linux_amd64.apk"
sudo apk add --allow-untrusted "slk_${VERSION}_linux_amd64.apk"
```

### Tarball (any distro)

Swap `x86_64` for `arm64` on ARM:

```bash
VERSION=$(curl -fsSL https://api.github.com/repos/gammons/slk/releases/latest | grep -oE '"tag_name": *"v[^"]+"' | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | sed 's/^v//')
curl -fsSL "https://github.com/gammons/slk/releases/latest/download/slk_${VERSION}_linux_x86_64.tar.gz" | tar xz
sudo mv slk /usr/local/bin/
```

## macOS

```bash
VERSION=$(curl -fsSL https://api.github.com/repos/gammons/slk/releases/latest | grep -oE '"tag_name": *"v[^"]+"' | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | sed 's/^v//')
# Apple Silicon
curl -fsSL "https://github.com/gammons/slk/releases/latest/download/slk_${VERSION}_darwin_arm64.tar.gz" | tar xz
# Intel
curl -fsSL "https://github.com/gammons/slk/releases/latest/download/slk_${VERSION}_darwin_x86_64.tar.gz" | tar xz

sudo mv slk /usr/local/bin/
```

## Windows

Download the `windows_x86_64.zip` from the [latest release](https://github.com/gammons/slk/releases/latest), extract `slk.exe`, and add it to your `PATH`.

## Go

```bash
go install github.com/gammons/slk/cmd/slk@latest
```

## Build from source

Requires Go 1.22+.

On Linux, `Ctrl+V` paste-to-upload needs slightly different setup depending on your session type.

**X11 sessions** use the `golang.design/x/clipboard` library, which requires X11 development headers at build time:

- Debian/Ubuntu: `sudo apt-get install -y libx11-dev`
- Fedora/RHEL: `sudo dnf install -y libX11-devel`
- Arch: included in `xorg-server`

**Wayland sessions** bypass the X11 library entirely and shell out to `wl-paste` from the `wl-clipboard` package — install it for paste-to-upload to work:

- Debian/Ubuntu: `sudo apt-get install -y wl-clipboard`
- Fedora/RHEL: `sudo dnf install -y wl-clipboard`
- Arch: `sudo pacman -S wl-clipboard`

slk auto-detects the session via `WAYLAND_DISPLAY` at startup. On headless Linux (or when neither dependency is met), slk runs but `Ctrl+V` smart-paste is disabled.

```bash
git clone https://github.com/gammons/slk.git
cd slk
make build       # binary at bin/slk
```

## Verify your download

```bash
curl -fsSLO https://github.com/gammons/slk/releases/latest/download/checksums.txt
sha256sum -c checksums.txt --ignore-missing
```

## Next steps

After installation, head to **[[Setup]]** to add your first workspace.
