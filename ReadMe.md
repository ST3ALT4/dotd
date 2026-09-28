# dotd

A small daemon that keeps your `~/Downloads` folder tidy. New files are moved into folders by type, so PDFs go to `docs/pdf`, images to `images/png`, and so on.

## Install

Requires Go and a Linux system with systemd.

```bash
git clone https://github.com/ST3ALT4/dotd.git
cd dotd
go build -o ~/.local/bin/dotd .
```

Make sure `~/.local/bin` is in your `PATH`.

## Usage

```bash
dotd start       # start in the background
dotd stop        # stop it
dotd status      # is it running? does it start on boot?
dotd install     # start on boot and start now
dotd uninstall   # remove start on boot
dotd run         # run in the foreground
```

To have it start automatically when your system boots, run `dotd install` once.

## Configuration

On first run, dotd creates `~/.config/dotd/conf.json`:

```json
{
  "files": [
    { "extension": ".pdf", "location": "docs/pdf" },
    { "extension": ".png", "location": "images/png" }
  ]
}
```

Each entry maps a file extension to a folder inside `~/Downloads`. Edit the file to add your own, then restart dotd:

```bash
dotd stop && dotd start
```

Default types: `.pdf` `.png` `.jpg` `.txt` `.md` `.xlsx` `.pptx` `.html` `.js` `.py` `.zip` `.tar`

## How it works

- dotd checks the modified time of `~/Downloads` and only scans the folder when something changed.
- A file is moved only after its size stops changing, so downloads in progress are left alone.
- Unfinished browser downloads (`.crdownload`, `.part`, `.tmp`) are ignored.
- If a file with the same name already exists, the new one is renamed, like `report (1).pdf`.
- Only files directly in `~/Downloads` are moved. Existing subfolders are not touched.

## Logs

- Started with `dotd install`: `journalctl --user -u dotd`
- Started with `dotd start`: `~/.config/dotd/dotd.log`

## Uninstall

```bash
dotd uninstall
rm ~/.local/bin/dotd
rm -r ~/.config/dotd
```
