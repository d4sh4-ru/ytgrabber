#!/bin/sh
# Installs YT Grabber for the current user (no root needed): the binary
# into ~/.local/bin, plus an application menu entry with an icon.
# Run from the unpacked release archive: ./install.sh
set -eu

here=$(cd "$(dirname "$0")" && pwd)
bin_dir="${XDG_BIN_HOME:-$HOME/.local/bin}"
data_dir="${XDG_DATA_HOME:-$HOME/.local/share}"
icon="$data_dir/icons/ytgrabber.png"

mkdir -p "$bin_dir" "$data_dir/applications" "$data_dir/icons"
install -m755 "$here/ytgrabber" "$bin_dir/ytgrabber"
install -m644 "$here/ytgrabber.png" "$icon"
sed -e "s|^Exec=.*|Exec=$bin_dir/ytgrabber|" -e "s|^Icon=.*|Icon=$icon|" \
    "$here/ytgrabber.desktop" > "$data_dir/applications/ytgrabber.desktop"

echo "Установлено: $bin_dir/ytgrabber"
echo "Нужен WebKitGTK 4.1. Debian/Ubuntu:"
echo "  sudo apt install libwebkit2gtk-4.1-0 gstreamer1.0-plugins-good gstreamer1.0-libav"
