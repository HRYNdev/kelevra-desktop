#!/usr/bin/env bash
# Стенд «пункт меню трея ставит обновление по-настоящему».
#
# Диагноз (30.08): установка обновления открыта ТРЕМЯ дверями. Кнопка в окне
# покрыта stend/oblik_ustanovka_obnovleniya.py. Обе двери в ТРЕЕ —
# пузырь (NIN_BALLOONUSERCLICK -> tychokVPuzyr, cmd/kelevra/trey_windows.go:451)
# и ПУНКТ МЕНЮ (idMenuObnovit=1003, добавляется punktMenyuObnovleniya() из
# metka_obnovleniya.go, обработка — trey_windows.go: case idMenuObnovit ->
# tychokVObnovlenie("пункт меню")) — не были покрыты ничем, кроме юнит-теста
# на ТЕКСТ метки (metka_obnovleniya_test.go): что кнопка появится в меню
# доказано, что клик по ней реально СТАВИТ обновление — нет. Клик по пузырю
# под wine не долетает (замерено дважды, см. шапку stend/trey_zhivoy.sh) —
# этот стенд его не трогает.
#
# ЧТО ДОКАЗЫВАЕТ. Настоящая windows-сборка под wine+Xvfb+explorer:
#   1) фон находит обновление и в контекстном меню трея (правый клик по
#      иконке) появляется пункт «Обновить до X»;
#   2) НАСТОЯЩИЙ клик мышью (xdotool) сперва по иконке (правая кнопка —
#      открывает меню), потом по САМОМУ ПУНКТУ меню в открывшемся popup —
#      это НЕ подмена: TrackPopupMenu у wine рисует настоящее X11-окно
#      меню, и клик по нему замерен живьём 30.08 — он доходит до WndProc
#      (case wmCommand/idMenuObnovit) и вызывает tychokVObnovlenie ровно
#      так же, как настоящая Windows доставляет выбор пункта. WM_COMMAND
#      руками тут не нужен — живой клик работает.
#   3) файл .exe работающей копии на диске РЕАЛЬНО подменяется на новую
#      сборку — судим по sha256, не по строке в журнале (строку печатаем
#      дополнительно).
#
# Источник релизов — поддельный локальный http.server (как в
# stend/obnovlenie_fon.sh и stend/oblik_ustanovka_obnovleniya.py): здесь
# проверяется ДВЕРЬ трея, не сеть GitHub.
#
#   stend/trey_punkt_obnovleniya.sh            — зелёный прогон
#   stend/trey_punkt_obnovleniya.sh --kontrol  — портит ПРОДУКТ на время своей
#     сборки: временно сдвигает id, который слушает case в WndProc, так что
#     пункт меню виден и кликается, а обработчик его больше не узнаёт (та же
#     дверь, подмененный замок). Файл возвращается к исходному ДО следующего
#     шага, что бы ни случилось — доказывает: стенд умеет краснеть, а не
#     просто печатает «зелёный» по привычке.
set -u
KOREN=$(cd "$(dirname "$0")/.." && pwd)
WINE=${WINE:-/usr/lib/wine/wine64}
export WINEPREFIX=${WINEPREFIX:-$KOREN/.wine}
export WINEDEBUG=${WINEDEBUG:--all}
export HOME=${HOME:-/root}
export LANG=${LANG:-C.UTF-8} LC_ALL=${LC_ALL:-C.UTF-8}
export GOCACHE=${GOCACHE:-/tmp/gocache}
command -v go >/dev/null 2>&1 || export PATH="$PATH:/usr/local/go/bin"
STEND="$KOREN/.stend/trey_punkt_obnovleniya"
mkdir -p "$STEND" "$WINEPREFIX"
. "$KOREN/stend/obshchee.sh"

if [ ! -x "$WINE" ]; then
  echo "нет wine ($WINE): apt-get install -y --no-install-recommends wine64" >&2
  exit 2
fi
command -v xdotool >/dev/null 2>&1 || { echo "нет xdotool: apt-get install -y xdotool" >&2; exit 2; }

KONTROL=0
case "${1:-}" in
  --kontrol) KONTROL=1 ;;
  "") ;;
  *) echo "аргумент не понят: $1 (жду --kontrol)" >&2; exit 2 ;;
esac

STARAYA_VERSIYA="0.1.0-rabota"   # версия сборки без -X podpiska.Versiya
NOVAYA_VERSIYA="9.9.9"
PIDY=()

pochistit() {
  for pid in "${PIDY[@]:-}"; do
    [ -n "$pid" ] && kill -KILL "$pid" 2>/dev/null
  done
  for pid in "${PIDY[@]:-}"; do
    [ -n "$pid" ] && wait "$pid" 2>/dev/null
  done
  # POSIX-путь не совпадает с argv Windows-процесса под wine (та же гра***,
  # что в trey_zhivoy.sh) — бьём по хвосту имени, не по своему пути.
  pkill -TERM -f '[K]elevra\.exe' 2>/dev/null
  for _ in $(seq 1 5); do
    pgrep -f '[K]elevra\.exe' >/dev/null 2>&1 || break
    sleep 1
  done
  pkill -KILL -f '[K]elevra\.exe' 2>/dev/null
  "$(dirname "$WINE")/wineserver64" -k 2>/dev/null || "$(dirname "$WINE")/wineserver" -k 2>/dev/null
  rm -rf "$STEND"
}
otkatit_porchu() { :; } # переопределяется ниже, если понадобится (--kontrol)
trap 'otkatit_porchu; pochistit' EXIT

DISPLAY_TREYA=:95
if ! xdpyinfo -display "$DISPLAY_TREYA" >/dev/null 2>&1; then
  Xvfb "$DISPLAY_TREYA" -screen 0 1280x800x24 >/dev/null 2>&1 &
  PIDY+=("$!")
  sleep 2
fi
if ! xdpyinfo -display "$DISPLAY_TREYA" >/dev/null 2>&1; then
  echo "⚫ ПРИБОР МЁРТВ: Xvfb $DISPLAY_TREYA не поднялся — трей проверить негде"
  exit 7
fi

rm -rf "$STEND"; mkdir -p "$STEND/dom" "$STEND/relizy"

TREY_SRC="$KOREN/cmd/kelevra/trey_windows.go"
otkatit_porchu() { # безусловный откат исходника — вызывается и при выходе, и явно
  if [ -f "$STEND/trey_windows.go.original" ]; then
    cp "$STEND/trey_windows.go.original" "$TREY_SRC"
  fi
}
trap 'otkatit_porchu; pochistit' EXIT

echo "── сборка Kelevra.exe (windows/amd64, версия $STARAYA_VERSIYA) ──"
if [ "$KONTROL" = "1" ]; then
  # Порча ПРОДУКТА, не окружения: пункт «Обновить до X» в меню появляется и
  # выглядит как обычно (AppendMenuW всё ещё вешает на него idMenuObnovit),
  # но case в WndProc, который должен его узнать, временно слушает ЧУЖОЙ id
  # (idMenuObnovit+1) — тот же замок двери подменён с другой стороны. Клик
  # по видимому пункту меню уйдёт в WM_COMMAND(1003), а обработчик ждёт 1004.
  cp "$TREY_SRC" "$STEND/trey_windows.go.original"
  sed -i 's/case idMenuObnovit:/case idMenuObnovit + 1:/' "$TREY_SRC"
  if ! grep -q 'case idMenuObnovit + 1:' "$TREY_SRC"; then
    echo "⚫ ПРИБОР МЁРТВ: не нашёл строку 'case idMenuObnovit:' — портить нечего, стенд не может это доказать"
    exit 7
  fi
  echo "  (порча: WndProc временно слушает idMenuObnovit+1 вместо idMenuObnovit — пункт меню виден, но не узнан)"
fi
if ! GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui -X github.com/HRYNdev/kelevra-desktop/internal/podpiska.Versiya=$STARAYA_VERSIYA" \
    -o "$STEND/Kelevra.exe" "$KOREN/cmd/kelevra" > "$STEND/build_staraya.log" 2>&1; then
  echo "  НЕ СОБРАЛСЯ"; cat "$STEND/build_staraya.log"
  otkatit_porchu
  exit 1
fi
otkatit_porchu # исходник возвращён СРАЗУ после сборки — бинарь уже несёт порчу, дереву больше не грозит ничего
SHA_STARAYA=$(sha256sum "$STEND/Kelevra.exe" | awk '{print $1}')

echo "── сборка релиза Kelevra.exe (версия $NOVAYA_VERSIYA, для поддельного источника) ──"
if ! GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui -X github.com/HRYNdev/kelevra-desktop/internal/podpiska.Versiya=$NOVAYA_VERSIYA" \
    -o "$STEND/relizy/Kelevra.exe" "$KOREN/cmd/kelevra" > "$STEND/build_novaya.log" 2>&1; then
  echo "  НЕ СОБРАЛСЯ"; cat "$STEND/build_novaya.log"; exit 1
fi
SHA_NOVAYA=$(sha256sum "$STEND/relizy/Kelevra.exe" | awk '{print $1}')
RAZMER=$(stat -c%s "$STEND/relizy/Kelevra.exe")
if [ "$SHA_STARAYA" = "$SHA_NOVAYA" ]; then
  echo "⚫ ПРИБОР МЁРТВ: старая и новая сборки совпали байт в байт — заменой нечего доказывать"
  exit 7
fi

PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
printf '[{"tag_name":"app-v%s","draft":false,"prerelease":false,"assets":[{"name":"Kelevra.exe","browser_download_url":"http://127.0.0.1:%s/Kelevra.exe","size":%s}]}]' \
  "$NOVAYA_VERSIYA" "$PORT" "$RAZMER" > "$STEND/relizy/relizy.json"
python3 -m http.server "$PORT" --directory "$STEND/relizy" >/dev/null 2>&1 &
PIDY+=("$!")
sleep 1

DOM="$STEND/dom"
ZHURNAL="$DOM/kelevra.log"
EXE="$DOM/Kelevra.exe"
cp "$STEND/Kelevra.exe" "$EXE"

echo "── запуск --sluzhba под wine (DISPLAY=$DISPLAY_TREYA, поддельный релиз-сервер на 127.0.0.1:$PORT) ──"
wine_zapusti "$STEND/zapusk.log" "$ZHURNAL" "трей: Shell_NotifyIconW(NIM_ADD)" 15 -- \
  env DISPLAY="$DISPLAY_TREYA" KELEVRA_DIR="$DOM" KELEVRA_BEZ_OKNA=1 KELEVRA_BEZ_OBNOVLENIYA=1 \
  KELEVRA_RELIZY="http://127.0.0.1:$PORT/relizy.json" KELEVRA_PERIOD_OBNOVLENIYA=2s \
  timeout 40 "$WINE" "$EXE"
rc=$?
PID_SLUZHBY=$WINE_ZAPUSTI_PID
PIDY+=("$PID_SLUZHBY")
if [ "$rc" -eq 77 ]; then
  echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
  exit 7
fi

echo "── ждём находку версии $NOVAYA_VERSIYA в журнале ──"
naiden=""
for _ in $(seq 1 15); do
  grep -qF "фоновая проверка обновления: найдена версия $NOVAYA_VERSIYA" "$ZHURNAL" 2>/dev/null && { naiden=1; break; }
  kill -0 "$PID_SLUZHBY" 2>/dev/null || break
  sleep 1
done
if [ -z "$naiden" ]; then
  echo "🔴 ПРОВАЛ: фон не нашёл $NOVAYA_VERSIYA за 15с"; tail -30 "$ZHURNAL" 2>/dev/null
  exit 1
fi
echo "  🟢 фон нашёл $NOVAYA_VERSIYA"

echo "── ищем иконку в системном трее (explorer.exe, полоса ниже 40px) ──"
naiti_ikonku_treya() {
  DISPLAY="$DISPLAY_TREYA" xwininfo -root -tree 2>/dev/null \
    | grep -oE '[0-9]+x[0-9]+\+[0-9]+\+[0-9]+' \
    | while IFS='x+' read -r w h x y; do
        if [ "$h" -ge 15 ] && [ "$h" -le 40 ] && [ "$w" -ge 20 ]; then
          echo "$((x + 10)) $((y + 10))"
          break
        fi
      done
}
KOORD=""
for _ in $(seq 1 10); do
  KOORD=$(naiti_ikonku_treya)
  [ -n "$KOORD" ] && break
  sleep 1
done
if [ -z "$KOORD" ]; then
  echo "⚫ ПРИБОР МЁРТВ: explorer.exe/systray под $DISPLAY_TREYA не поднялся за 10с — кликать некуда"
  exit 7
fi
echo "  иконка трея: $KOORD"

echo "── настоящий клик мышью: правая кнопка по иконке -> контекстное меню ──"
# shellcheck disable=SC2086
DISPLAY="$DISPLAY_TREYA" xdotool mousemove $KOORD click 3
sleep 1.5
IKONKA_X=${KOORD%% *}; IKONKA_Y=${KOORD##* }
naiti_menyu() { # печатает "X Y WxH" popup-окна TrackPopupMenu рядом с иконкой
  DISPLAY="$DISPLAY_TREYA" xwininfo -root -tree 2>/dev/null \
    | grep -oE '[0-9]+x[0-9]+\+[0-9]+\+[0-9]+' \
    | while IFS='x+' read -r w h x y; do
        # popup меню — окно ЗАМЕТНО выше строки иконок (>40px) и не 1x1 служебное
        if [ "$h" -gt 40 ] && [ "$w" -gt 20 ] && [ "$x" -ge $((IKONKA_X - 20)) ]; then
          echo "$x $y $w $h"
          break
        fi
      done
}
MENYU=""
for _ in $(seq 1 8); do
  MENYU=$(naiti_menyu)
  [ -n "$MENYU" ] && break
  sleep 0.5
done
if [ -z "$MENYU" ]; then
  echo "🔴 ПРОВАЛ: контекстное меню не появилось после правого клика по $KOORD"
  exit 1
fi
read -r MX MY MW MH <<<"$MENYU"
# «Обновить до X» — ПЕРВЫЙ пункт меню (punktMenyuObnovleniya вставлен перед
# «Открыть»/«Выход», см. trey_windows.go: pokazatMenuTreya) — координата
# внутри верхней трети окна меню.
KLIK_X=$((MX + 15))
KLIK_Y=$((MY + 10))
echo "  меню: ${MW}x${MH}+${MX}+${MY} — кликаю по первому пункту ($KLIK_X, $KLIK_Y)"

echo "── настоящий клик мышью: по пункту «Обновить до $NOVAYA_VERSIYA» ──"
DISPLAY="$DISPLAY_TREYA" xdotool mousemove "$KLIK_X" "$KLIK_Y" click 1
sleep 2

if ! grep -qF 'трей: «Обновить» из меню' "$ZHURNAL" 2>/dev/null; then
  echo "🔴 ПРОВАЛ: клик по пункту меню не дошёл до WndProc — строки «трей: «Обновить» из меню» нет"
  tail -30 "$ZHURNAL"
  exit 1
fi
echo "  🟢 клик дошёл: журнал знает про «Обновить» из меню"

echo "── ждём итог установки (файл на диске обязан подмениться, либо служба честно отказаться) ──"
for _ in $(seq 1 15); do
  grep -qE 'тычок \(пункт меню\)' "$ZHURNAL" 2>/dev/null && break
  sleep 1
done
sleep 1
tail -10 "$ZHURNAL"

if [ ! -f "$EXE" ]; then
  echo "🔴 ПРОВАЛ: файл $EXE исчез с диска после установки — не осталось РАБОТАЮЩЕГО приложения"
  exit 1
fi
SHA_POSLE=$(sha256sum "$EXE" | awk '{print $1}')
echo "── ФАКТ НА ДИСКЕ: sha256 до=$SHA_STARAYA после=$SHA_POSLE релиз=$SHA_NOVAYA ──"
if [ "$SHA_POSLE" != "$SHA_NOVAYA" ] || [ "$SHA_POSLE" = "$SHA_STARAYA" ]; then
  echo "🔴 ПРОВАЛ: файл на диске НЕ стал байт-в-байт новой сборкой — клик по пункту меню не довёл дело до Postavit()"
  exit 1
fi

echo
echo "── итог: ЗЕЛЁНЫЙ ──"
echo "Правый клик по иконке трея открыл настоящее popup-меню (TrackPopupMenu), клик по"
echo "пункту «Обновить до $NOVAYA_VERSIYA» дошёл до WndProc (case wmCommand/idMenuObnovit)"
echo "и файл работающей копии на диске подменился байт-в-байт на новую сборку."
exit 0
