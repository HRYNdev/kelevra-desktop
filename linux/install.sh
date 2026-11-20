#!/bin/sh
# Kelevra: установка на Linux одной командой.
#
#   curl -fsSL https://github.com/HRYNdev/kelevra-desktop/releases/latest/download/install.sh | sh
#
# Что делает: ставит WebKitGTK 4.1 и nftables пакетным менеджером дистрибутива
# (apt, dnf, pacman, zypper), скачивает kelevra-linux-amd64 из последнего
# выпуска, сверяет подпись ed25519 тем же открытым ключом, что вшит в
# приложение (internal/podpis), и запускает установку службы
# (kelevra --postavit-sluzhbu: /opt/kelevra, служба systemd, ярлык в меню).
# Дальше программа обновляется сама, как обычно.
#
# Второй режим — для Kelevra.AppImage (linux/AppRun): уже от root, с готовой
# программой из образа, без скачивания:
#   install.sh --iz-fayla <путь к kelevra>
#
# Переменные: KELEVRA_TEG=app-vX.Y.Z — взять не последний выпуск, а этот
# (проба на стенде).
set -eu

REPO="HRYNdev/kelevra-desktop"
IMYA="kelevra-linux-amd64"
# Открытый ключ проверки подписи = podpis.KlyuchHex
# (f31f77c374013c6bdc4548896672868722a1ffb611d86604d78a4bb37d8753d1) в виде PEM.
KLYUCH_PEM="-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEA8x93w3QBPGvcRUiJZnKGhyKh/7YR2GYE14pLs32HU9E=
-----END PUBLIC KEY-----"

skazat() { printf '%s\n' "$*"; }
provalit() {
	printf '\nОшибка: %s\n' "$*" >&2
	exit 1
}

# ── Права администратора ─────────────────────────────────────────────────────
KAK=""
kak_root() {
	if [ "$(id -u)" = 0 ]; then
		KAK=""
		return
	fi
	if command -v sudo >/dev/null 2>&1 && sudo -v; then
		KAK="sudo"
		return
	fi
	if command -v pkexec >/dev/null 2>&1; then
		KAK="pkexec"
		return
	fi
	provalit "нужны права администратора: нет ни sudo, ни pkexec. Выполните команду от root."
}
root() {
	if [ -n "$KAK" ]; then
		$KAK "$@"
	else
		"$@"
	fi
}

# ── Пакеты ───────────────────────────────────────────────────────────────────
est_biblioteka() {
	for lc in /sbin/ldconfig /usr/sbin/ldconfig ldconfig; do
		if command -v "$lc" >/dev/null 2>&1 || [ -x "$lc" ]; then
			"$lc" -p 2>/dev/null | grep -q "$1" && return 0
			return 1
		fi
	done
	return 1
}
est_komanda() {
	command -v "$1" >/dev/null 2>&1 || [ -x "/usr/sbin/$1" ] || [ -x "/sbin/$1" ]
}

postavit_pakety() {
	nuzhno_webkit=0
	nuzhno_nft=0
	nuzhno_openssl=0
	est_biblioteka 'libwebkit2gtk-4\.1\.so\.0' || nuzhno_webkit=1
	est_komanda nft || nuzhno_nft=1
	if [ "$1" = s_podpisyu ]; then
		openssl version 2>/dev/null | grep -q '^OpenSSL [3-9]' || nuzhno_openssl=1
	fi
	if [ $nuzhno_webkit = 0 ] && [ $nuzhno_nft = 0 ] && [ $nuzhno_openssl = 0 ]; then
		skazat "Нужные компоненты уже установлены."
		return
	fi
	if command -v apt-get >/dev/null 2>&1; then
		pm="apt"
		webkit="libwebkit2gtk-4.1-0"
	elif command -v dnf >/dev/null 2>&1; then
		pm="dnf"
		webkit="webkit2gtk4.1"
	elif command -v pacman >/dev/null 2>&1; then
		pm="pacman"
		webkit="webkit2gtk-4.1"
	elif command -v zypper >/dev/null 2>&1; then
		pm="zypper"
		webkit="libwebkit2gtk-4_1-0"
	else
		provalit "пакетный менеджер не распознан (нужен apt, dnf, pacman или zypper). Установите WebKitGTK 4.1 и nftables вручную и повторите команду."
	fi
	spisok=""
	[ $nuzhno_webkit = 1 ] && spisok="$spisok $webkit"
	[ $nuzhno_nft = 1 ] && spisok="$spisok nftables"
	[ $nuzhno_openssl = 1 ] && spisok="$spisok openssl"
	skazat "Установка компонентов:$spisok"
	case "$pm" in
	apt)
		root env DEBIAN_FRONTEND=noninteractive apt-get update -q || provalit "apt-get update не выполнен"
		# shellcheck disable=SC2086
		root env DEBIAN_FRONTEND=noninteractive apt-get install -y -q $spisok || provalit "пакеты не установлены:$spisok"
		;;
	dnf)
		# shellcheck disable=SC2086
		root dnf install -y $spisok || provalit "пакеты не установлены:$spisok"
		;;
	pacman)
		# shellcheck disable=SC2086
		root pacman -S --needed --noconfirm $spisok || provalit "пакеты не установлены:$spisok (возможно, нужна sudo pacman -Syu)"
		;;
	zypper)
		# shellcheck disable=SC2086
		root zypper --non-interactive install $spisok || provalit "пакеты не установлены:$spisok"
		;;
	esac
	est_biblioteka 'libwebkit2gtk-4\.1\.so\.0' || provalit "WebKitGTK 4.1 не найдена после установки — дистрибутив слишком старый"
}

# ── Скачивание и подпись ─────────────────────────────────────────────────────
skachat() {
	if command -v curl >/dev/null 2>&1; then
		curl -fSL --retry 3 --progress-bar -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q --show-progress -O "$2" "$1"
	else
		provalit "нет curl и wget"
	fi
}

proverit_podpis() {
	fayl="$1"
	papka="$(dirname "$fayl")"
	sha="$(sha256sum "$fayl" | cut -d' ' -f1)"
	printf 'kelevra-podpis-v1\n%s\n%s' "$IMYA" "$sha" >"$papka/soobshchenie"
	printf '%s\n' "$KLYUCH_PEM" >"$papka/klyuch.pem"
	base64 -d "$fayl.sig" >"$papka/podpis.bin" 2>/dev/null || provalit "файл подписи повреждён"
	if ! openssl pkeyutl -verify -pubin -inkey "$papka/klyuch.pem" -rawin \
		-in "$papka/soobshchenie" -sigfile "$papka/podpis.bin" >/dev/null 2>&1; then
		provalit "подпись не сходится: файл не из выпуска Kelevra. Установка остановлена."
	fi
	skazat "Подпись верна (sha256 $sha)."
}

# ── Главное ──────────────────────────────────────────────────────────────────
proverit_sistemu() {
	[ "$(uname -s)" = Linux ] || provalit "эта установка — только для Linux"
	case "$(uname -m)" in
	x86_64 | amd64) ;;
	*) provalit "поддерживается только 64-битный x86 (у вас $(uname -m))" ;;
	esac
	[ -d /run/systemd/system ] || provalit "нужен systemd: служба Kelevra работает под ним"
}

ustanovit_sluzhbu() {
	chmod 755 "$1"
	skazat "Установка службы Kelevra…"
	root "$1" --postavit-sluzhbu || provalit "служба не установлена (подробности — в журнале: journalctl -u kelevra)"
	if systemctl is-active --quiet kelevra 2>/dev/null; then
		skazat "Служба Kelevra запущена."
	else
		provalit "служба Kelevra не запустилась (journalctl -u kelevra)"
	fi
}

if [ "${1:-}" = "--iz-fayla" ]; then
	# Режим AppImage: уже root (pkexec), программа — рядом.
	[ -n "${2:-}" ] || provalit "не указан файл программы"
	proverit_sistemu
	kak_root
	postavit_pakety bez_podpisi
	ustanovit_sluzhbu "$2"
	exit 0
fi

skazat "Установка Kelevra"
proverit_sistemu
kak_root
postavit_pakety s_podpisyu

if [ -n "${KELEVRA_TEG:-}" ]; then
	adres="https://github.com/$REPO/releases/download/$KELEVRA_TEG"
else
	adres="https://github.com/$REPO/releases/latest/download"
fi
vrem="$(mktemp -d)"
trap 'rm -rf "$vrem"' EXIT INT TERM
skazat "Загрузка Kelevra…"
skachat "$adres/$IMYA" "$vrem/$IMYA" || provalit "не удалось скачать $adres/$IMYA"
skachat "$adres/$IMYA.sig" "$vrem/$IMYA.sig" || provalit "не удалось скачать подпись"
proverit_podpis "$vrem/$IMYA"
ustanovit_sluzhbu "$vrem/$IMYA"

if [ -n "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ] && [ "$(id -u)" != 0 ]; then
	(setsid /opt/kelevra/kelevra >/dev/null 2>&1 &) || true
	skazat ""
	skazat "Готово. Окно Kelevra открывается; дальше — в меню приложений: Kelevra."
else
	skazat ""
	skazat "Готово. Kelevra — в меню приложений."
fi
