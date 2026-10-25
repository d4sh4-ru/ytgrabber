#!/bin/sh
# Renders the raster icons from appicon.svg / appicon-small.svg:
#   build/appicon.png        1024 px; Wails makes the macOS .icns from it,
#                            Linux uses it for the window and menu entry
#   build/windows/icon.ico   16–256 px for the exe and the installer
#   frontend/public/favicon.ico
# Needs Chromium (or Chrome) and ImageMagick: CHROME=/path/to/chrome ./build/icons.sh
set -eu
cd "$(dirname "$0")"
chrome="${CHROME:-chromium}"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

render() { # svg size out
	printf '<html><body style="margin:0"><img src="%s" width="%s" height="%s"></body></html>' \
		"file://$PWD/$1" "$2" "$2" > "$tmp/page.html"
	"$chrome" --headless=new --no-sandbox --disable-gpu --hide-scrollbars \
		--default-background-color=00000000 --window-size="$2,$2" \
		--screenshot="$3" "file://$tmp/page.html" 2>/dev/null
}

# Always at full size: headless Chrome can't shrink its window enough for
# small renders, so they get cropped. ImageMagick scales down instead.
render appicon.svg 1024 appicon.png
cp appicon.png "$tmp/large.png"
render appicon-small.svg 1024 "$tmp/small.png"

for size in 16 24 32 48; do
	convert "$tmp/small.png" -resize "${size}x${size}" "$tmp/$size.png"
done
for size in 64 128 256; do
	convert "$tmp/large.png" -resize "${size}x${size}" "$tmp/$size.png"
done
convert "$tmp/16.png" "$tmp/24.png" "$tmp/32.png" "$tmp/48.png" "$tmp/64.png" "$tmp/128.png" "$tmp/256.png" windows/icon.ico
convert "$tmp/16.png" "$tmp/32.png" "$tmp/48.png" ../frontend/public/favicon.ico
echo "icons updated"
