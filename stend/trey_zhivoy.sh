#!/usr/bin/env bash
# Стенд «живой трей»: доказывает живьём (настоящая windows-сборка под wine),
# то, чего линуксовая заглушка (trey_other.go, судит его stend/trey_oblachko.sh)
# и «сухой» stend/trey.sh НИКОГДА не проверяли — реальный WndProc и реальный
# wine-explorer с настоящим системным треем поверх Xvfb.
#
# Диагноз 26.08: весь путь трея был доказан ДВАЖДЫ, и оба раза мимо
# trey_windows.go — trey_oblachko.sh считает строки линуксовой заглушки,
# trey.sh честно пишет в своей шапке «explorer.exe тут нет вовсе». Из-за
# такой же слепоты уехала сборка, названная готовой, а она делала
# запрещённое. Замер сегодня (следователь, живой Xvfb+xdotool) впервые
# исполнил cmd/kelevra/trey_windows.go целиком под wine и нашёл: wine САМ
# поднимает explorer.exe /desktop по требованию Shell_NotifyIconW (никакой
# ручной команды не нужно) и РИСУЕТ настоящий баллон с нашим текстом.
#
# Что стенд ДОКАЗЫВАЕТ (только это, не больше):
#   1) настоящая windows-сборка под wine+Xvfb доводит вызов до
#      Shell_NotifyIconW(NIM_MODIFY, NIF_INFO) — пузырь про находку — и он
#      возвращает true, а не молчит и не падает;
#   2) клик мышью (xdotool) по САМОЙ ИКОНКЕ в трее долетает до нашего
#      WndProc как ninSelect и запускает «Открыть» (otkrytOknoIzTreya) —
#      то есть события мыши от wine-трея до нашего кода вообще доходят.
#
# ЧЕГО ЭТОТ СТЕНД НЕ ДОКАЗЫВАЕТ. Клик по ПУЗЫРЮ (NIN_BALLOONUSERCLICK,
# tychokVPuzyr → установка) здесь НЕ проверяется и не может: замерено дважды
# (следователь, xdotool по видимым координатам баллона со свежим
# xwd-снимком экрана, доказывающим, что баллон в кадре в момент клика) —
# wine-овский explorer РИСУЕТ баллон, но клика по нему НЕ ловит и
# NIN_BALLOONUSERCLICK нам не шлёт. Клик по иконке (сцена 2 ниже) в это же
# самое время доходит без сбоя — значит доставка сообщений трея работает, а
# конкретно баллон в wine некликабелен. Кто хочет доказать сам тычок в
# пузырь — тому нужна живая Windows, не эта машина.
#
#   stend/trey_zhivoy.sh          — обе сцены, зелёный прогон
#   stend/trey_zhivoy.sh --kontrol=a|b — намеренно портит ОЖИДАНИЕ одной
#     сцены (не продукт), чтобы доказать: сцена умеет краснеть, а не просто
#     печатает «зелёный» по привычке.
#     a — «GitHub» заранее отдаёт ТУ ЖЕ версию, что в сборке: находки нет,
#         пузырю не о чем говорить — сцена 1 обязана покраснеть.
#     b — клик уходит НЕ в иконку, а в заведомо пустую точку экрана —
#         сцена 2 обязана покраснеть (событие не долетает никуда).
set -u
KOREN=$(cd "$(dirname "$0")/.." && pwd)
WINE=${WINE:-/usr/lib/wine/wine64}
export WINEPREFIX=${WINEPREFIX:-$KOREN/.wine}
export WINEDEBUG=${WINEDEBUG:--all}
export HOME=${HOME:-/root}
export LANG=${LANG:-C.UTF-8} LC_ALL=${LC_ALL:-C.UTF-8}
export GOCACHE=${GOCACHE:-/tmp/gocache}
command -v go >/dev/null 2>&1 || export PATH="$PATH:/usr/local/go/bin"
STEND="$KOREN/.stend/trey_zhivoy"
mkdir -p "$STEND" "$WINEPREFIX"
. "$KOREN/stend/obshchee.sh"

if [ ! -x "$WINE" ]; then
  echo "нет wine ($WINE): apt-get install -y --no-install-recommends wine64" >&2
  exit 2
fi
command -v xdotool >/dev/null 2>&1 || { echo "нет xdotool: apt-get install -y xdotool" >&2; exit 2; }

KONTROL=""
case "${1:-}" in
  --kontrol=a|--kontrol=b) KONTROL=${1#--kontrol=} ;;
  "") ;;
  *) echo "аргумент не понят: $1 (жду --kontrol=a|b)" >&2; exit 2 ;;
esac

VSEGO=2
SCENA_N=0
PIDY=()

shag() { # $1 имя  $2 что замерено  $3 итог(текст)
  SCENA_N=$((SCENA_N + 1))
  printf 'сцена %d/%d: %s — %s — итог: 🟢 %s\n' "$SCENA_N" "$VSEGO" "$1" "$2" "$3"
}
past() { # $1 имя  $2 что замерено  $3 причина провала  [$4 доп. вывод]
  SCENA_N=$((SCENA_N + 1))
  printf 'сцена %d/%d: %s — %s — итог: 🔴 ПРОВАЛ: %s\n' "$SCENA_N" "$VSEGO" "$1" "$2" "$3" >&2
  if [ -n "${4:-}" ]; then
    printf -- '--- разбор ---\n%s\n--------------\n' "$4" >&2
  fi
  pochistit
  exit 1
}

pochistit() { # процессы и Xvfb этого стенда — не чужие: свой отдельный DISPLAY.
  for pid in "${PIDY[@]:-}"; do
    [ -n "$pid" ] && kill -KILL "$pid" 2>/dev/null
  done
  # НЕ "$STEND/Kelevra.exe": в wine это Windows-процесс, его argv у ядра —
  # "Z:\...\Kelevra.exe" (обратные слэши, буква диска), POSIX-путь туда не
  # попадает НИКОГДА, и такой pkill молча не находит вообще ничего (проверено
  # живьём: без этой правки вторая копия — та, что сцена 2 открывает кликом
  # по «Открыть», — переживала стенд). Ищем по хвосту имени, как proksi.sh.
  pkill -TERM -f '[K]elevra\.exe' 2>/dev/null
  for _ in $(seq 1 5); do
    pgrep -f '[K]elevra\.exe' >/dev/null 2>&1 || break
    sleep 1
  done
  pkill -KILL -f '[K]elevra\.exe' 2>/dev/null
  # wineserver держит explorer.exe/winedevice.exe, которых наш процесс поднял
  # неявно (сам wine, по требованию Shell_NotifyIconW) — их эти pkill-ы не
  # видят вовсе. "-k" гасит всю сессию текущего WINEPREFIX разом.
  "$(dirname "$WINE")/wineserver64" -k 2>/dev/null || "$(dirname "$WINE")/wineserver" -k 2>/dev/null
  for pid in "${PIDY[@]:-}"; do
    [ -n "$pid" ] && wait "$pid" 2>/dev/null
  done
  rm -rf "$STEND"
}
trap pochistit EXIT

# DISPLAY отдельный (:98, не общий :97 остальных wine-стендов): нужен ЧИСТЫЙ
# systray без чужих иконок, оставшихся от соседних прогонов — иначе клик по
# «первой иконке» мог бы попасть в чужую.
DISPLAY_TREYA=:98
if ! xdpyinfo -display "$DISPLAY_TREYA" >/dev/null 2>&1; then
  Xvfb "$DISPLAY_TREYA" -screen 0 1280x800x24 >/dev/null 2>&1 &
  PIDY+=("$!")
  sleep 2
fi
if ! xdpyinfo -display "$DISPLAY_TREYA" >/dev/null 2>&1; then
  echo "⚫ ПРИБОР МЁРТВ: Xvfb $DISPLAY_TREYA не поднялся — трей проверить негде"
  exit 7
fi

rm -rf "$STEND"; mkdir -p "$STEND"
echo "── сборка Kelevra.exe (windows/amd64) ──"
if ! GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui" -o "$STEND/Kelevra.exe" "$KOREN/cmd/kelevra" > "$STEND/build.log" 2>&1; then
  echo "  НЕ СОБРАЛСЯ"; cat "$STEND/build.log"; exit 1
fi

# «GitHub» — локальный HTTP, отдающий один поддельный релиз новее сборки
# (сборка тут без -X podpiska.Versiya, значит текущая версия — «0.1.0-rabota»,
# и любое «9.9.x» её обгоняет). --kontrol=a переписывает файл на версию
# сборки — находки нет, пузырю не о чем говорить.
VERSIYA_STARAYA="0.1.0-rabota"
VERSIYA_NOVAYA="9.9.9"
mkdir -p "$STEND/relizy"
pishi_relizy() { # pishi_relizy <версия-в-теге>
  printf '[{"tag_name":"app-v%s","draft":false,"prerelease":false,"assets":[{"name":"Kelevra.exe","browser_download_url":"http://127.0.0.1:%s/Kelevra.exe","size":42}]}]' \
    "$1" "$PORT" > "$STEND/relizy/relizy.json"
}
PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
if [ "$KONTROL" = "a" ]; then
  pishi_relizy "$VERSIYA_STARAYA"
  echo "  (порча a: поддельный GitHub отдаёт ТУ ЖЕ версию $VERSIYA_STARAYA — обновляться не на что)"
else
  pishi_relizy "$VERSIYA_NOVAYA"
fi
python3 -m http.server "$PORT" --directory "$STEND/relizy" >/dev/null 2>&1 &
PIDY+=("$!")
sleep 1

PAPKA="$WINEPREFIX/drive_c/users/$(whoami)/AppData/Local/Kelevra"
ZHURNAL="$PAPKA/kelevra.log"
rm -rf "$PAPKA"
bed=0

echo "── запуск --sluzhba под wine (DISPLAY=$DISPLAY_TREYA, GitHub поддельный на 127.0.0.1:$PORT) ──"
# KELEVRA_BEZ_OBNOVLENIYA=1 — тик ПЕРВОЙ проверки идёт не мгновенно на
# холодном старте (замерено сегодня: мгновенная проверка обгоняет создание
# окна трея, и «пузырь: окно не поднято» — гонка самого приложения, не
# продукт вопроса), а через KELEVRA_PERIOD_OBNOVLENIYA — этого времени
# достаточно, чтобы поток трея успел встать.
DISPLAY="$DISPLAY_TREYA" KELEVRA_BEZ_OKNA=1 KELEVRA_BEZ_OBNOVLENIYA=1 \
  KELEVRA_RELIZY="http://127.0.0.1:$PORT/relizy.json" KELEVRA_PERIOD_OBNOVLENIYA=3s \
  wine_zapusti "$STEND/zapusk.log" "$ZHURNAL" "трей: Shell_NotifyIconW(NIM_ADD)" 15 -- \
  env DISPLAY="$DISPLAY_TREYA" KELEVRA_BEZ_OKNA=1 KELEVRA_BEZ_OBNOVLENIYA=1 \
  KELEVRA_RELIZY="http://127.0.0.1:$PORT/relizy.json" KELEVRA_PERIOD_OBNOVLENIYA=3s \
  timeout 30 "$WINE" "$STEND/Kelevra.exe"
rc=$?
PID_SLUZHBY=$WINE_ZAPUSTI_PID
PIDY+=("$PID_SLUZHBY")
if [ "$rc" -eq 77 ]; then
  echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
  exit 7
fi

echo "── сцена 1: находка обновления рисует НАСТОЯЩИЙ Shell_NotifyIconW(NIM_MODIFY, NIF_INFO) ──"
STROKA_PUZYR="трей: пузырь про версию $VERSIYA_NOVAYA -> Shell_NotifyIconW(NIM_MODIFY) = true"
naiden=""
for _ in $(seq 1 12); do
  grep -qF "$STROKA_PUZYR" "$ZHURNAL" 2>/dev/null && { naiden=1; break; }
  kill -0 "$PID_SLUZHBY" 2>/dev/null || break
  sleep 1
done
if [ -z "$naiden" ]; then
  past "пузырь про обновление рисуется живьём" "в журнале строка «$STROKA_PUZYR»" \
    "строки нет за 12с ожидания" "$(tail -30 "$ZHURNAL" 2>/dev/null)"
fi
shag "пузырь про обновление рисуется живьём" "wine исполнил Shell_NotifyIconW(NIM_MODIFY, NIF_INFO) и вернул true" \
  "реальный код trey_windows.go, не заглушка и не мнение о коде"

echo "── сцена 2: клик по иконке трея (xdotool) доходит до нашего WndProc ──"
# Ищем полосу systray, которую wine сам поднял под Shell_NotifyIconW (explorer
# «/desktop» стартует по требованию, без ручной команды — замерено сегодня):
# узкое по высоте (иконки 16px) окно explorer.exe, не служебное 1x1.
naiti_ikonku_treya() { # печатает "X Y" — координаты клика по первой иконке
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
if [ "$KONTROL" = "b" ]; then
  echo "  (порча b: клик уходит НЕ в найденную иконку ($KOORD), а в заведомо пустую точку 700 500)"
  KOORD="700 500"
else
  echo "  иконка трея: $KOORD"
fi
STROKA_OTKRYT='трей: «Открыть» запустил отдельную копию без аргументов'
otkryt=""
for popytka in 1 2; do
  # shellcheck disable=SC2086
  DISPLAY="$DISPLAY_TREYA" xdotool mousemove $KOORD click 1
  sleep 2
  if grep -qF "$STROKA_OTKRYT" "$ZHURNAL" 2>/dev/null; then otkryt=1; break; fi
done
if [ -z "$otkryt" ]; then
  past "клик по иконке доходит до WndProc" "в журнале строка «$STROKA_OTKRYT» после клика (xdotool) по $KOORD" \
    "строки нет после 2 попыток" "$(tail -20 "$ZHURNAL" 2>/dev/null)"
fi
shag "клик по иконке доходит до WndProc" "xdotool щёлкнул по $KOORD, ninSelect дошёл до нашего кода и открыл окно" \
  "доставка событий трея от wine до приложения — не выдумка"

# Открытая по клику вторая копия — тоже настоящий процесс, гасим вместе с
# остальными: pochistit бьёт по "$STEND/Kelevra.exe" безусловно.

if [ -n "$KONTROL" ]; then
  printf '\n(--kontrol=%s должен был провалиться выше — если ты это читаешь, порча не сработала)\n' "$KONTROL"
  exit 1
fi
echo "── итог: ЗЕЛЁНЫЙ ($SCENA_N/$VSEGO сцен) ──"
exit 0
