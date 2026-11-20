#!/bin/sh
# Сборка Kelevra-x86_64.AppImage из готовой программы (CI: vypusk.yml, задание linux).
#   linux/sobrat-appimage.sh <kelevra-linux-amd64> <выход.AppImage> <appimagetool> <runtime-x86_64>
# appimagetool и runtime качает CI с закреплёнными sha256 (см. vypusk.yml).
# Внутри образа: программа, install.sh (установка WebKitGTK и службы от root),
# AppRun (что делать при запуске), ярлык и значок.
set -eu
programma="$1"
vyhod="$2"
tool="$3"
runtime="$4"
zdes="$(cd "$(dirname "$0")" && pwd)"
koren="$(cd "$zdes/.." && pwd)"
vrem="$(mktemp -d)"
trap 'rm -rf "$vrem"' EXIT
appdir="$vrem/Kelevra.AppDir"
mkdir -p "$appdir/usr/bin" "$appdir/usr/share/kelevra"
install -m 755 "$programma" "$appdir/usr/bin/kelevra"
install -m 755 "$zdes/install.sh" "$appdir/usr/share/kelevra/install.sh"
install -m 755 "$zdes/AppRun" "$appdir/AppRun"
install -m 644 "$zdes/kelevra.desktop" "$appdir/kelevra.desktop"
install -m 644 "$koren/internal/vinsluzhba/znachok-256.png" "$appdir/kelevra.png"
ln -s kelevra.png "$appdir/.DirIcon"
chmod 755 "$tool"
ARCH=x86_64 APPIMAGE_EXTRACT_AND_RUN=1 "$tool" --no-appstream --runtime-file "$runtime" "$appdir" "$vyhod"
chmod 755 "$vyhod"
ls -l "$vyhod"
