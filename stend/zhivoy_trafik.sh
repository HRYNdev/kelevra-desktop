#!/usr/bin/env bash
# Стенд «живой трафик»: доказывает боевую цепочку ЧЕРЕЗ САМО ПРИЛОЖЕНИЕ на
# Linux, а не на стабах и не на голом `sing-box check`.
#
# Зачем отдельно от остальных стендов. Ни один щуп в репозитории ни разу не
# поднимал НАСТОЯЩЕЕ ядро sing-box через саму программу и не пропускал через
# него живой пакет: windows.sh/proksi.sh/trey.sh гоняют настоящий .exe, но
# под wine, а wine не отдаёт ядру сетевые адаптеры — там ядро запускается и
# тут же падает на любом входе, который просит реальный сокет вовне процесса.
# konfig-тесты (TestYadroPrinimaetGotovyyKonfig) зовут настоящее ядро, но
# только `sing-box check` — это разбор конфига, не поднятая связь.
#
# На Linux то же самое приложение собирается и работает без окна (см. README,
# cmd/kelevra/zapusk_other.go, --sluzhba) — здесь этим и пользуемся: собираем
# `cmd/kelevra` под Linux, поднимаем его как настоящий процесс со своим
# изолированным домом (KELEVRA_DIR — отдельная папка внутри .stend/, никогда
# не настоящий ~/.local/share/kelevra), просим подключиться и гоняем через
# него один маленький живой пакет — с проверкой ДВУМЯ каналами (код ответа И
# счётчики трафика самого ядра), чтобы одна враньём не прикрыла другую.
#
# Режим — только прокси: LXC не поднимет TUN (`auto_route` требует адаптер,
# которого тут нет), поэтому права принудительно гасим тем же рычагом, что и
# остальные стенды на этой машине — KELEVRA_PRAVA=net (internal/prava/prava_other.go).
# Туннель этим стендом НЕ доказывается — это ограничение среды, а не программы,
# и то, что оно ограничение среды, видно по режиму в /api/sostoyanie: "proksi"
# наступает не из-за отказа, а потому что prava.Est() вернул false нарочно.
set -u
KOREN=$(cd "$(dirname "$0")/.." && pwd)
export PATH="$PATH:/usr/local/go/bin"
export HOME=${HOME:-/root}
export GOCACHE=${GOCACHE:-$HOME/.cache/go-build}

YADRO_SRC="$KOREN/.stend/sing-box-linux"
STEND="$KOREN/.stend/zhivoy_trafik"
DOM="$STEND/dom"
BIN="$STEND/kelevra_linux"
PROFIL="$KOREN/internal/konfig/testdata/profil_telefona.json"

VSEGO=6
SHAG_N=0
SLUZHBA_PID=""

shag() { # $1 имя  $2 что замерено  $3 итог(текст)
  SHAG_N=$((SHAG_N + 1))
  printf 'шаг %d/%d: %s — %s — итог: %s\n' "$SHAG_N" "$VSEGO" "$1" "$2" "$3"
}

past() { # $1 имя  $2 что замерено  $3 причина провала  [$4 доп. вывод для разбора]
  SHAG_N=$((SHAG_N + 1))
  printf 'шаг %d/%d: %s — %s — итог: ПРОВАЛ: %s\n' "$SHAG_N" "$VSEGO" "$1" "$2" "$3" >&2
  if [ -n "${4:-}" ]; then
    printf -- '--- разбор ---\n%s\n--------------\n' "$4" >&2
  fi
  exit 1
}

# pole <json-файл-или--> <ключ> — плоское поле верхнего уровня, python3 у нас
# уже стенд-инструмент (stend/oblik_snimok.py), лишней зависимости не тащим.
pole() {
  python3 -c '
import json, sys
src, klyuch = sys.argv[1], sys.argv[2]
d = json.load(open(src)) if src != "-" else json.load(sys.stdin)
v = d.get(klyuch, "")
print(v if not isinstance(v, bool) else str(v).lower())
' "$1" "$2"
}

pochistit() {
  if [ -n "$SLUZHBA_PID" ] && kill -0 "$SLUZHBA_PID" 2>/dev/null; then
    kill -TERM "$SLUZHBA_PID" 2>/dev/null
    for _ in $(seq 1 20); do
      kill -0 "$SLUZHBA_PID" 2>/dev/null || break
      sleep 0.5
    done
    kill -KILL "$SLUZHBA_PID" 2>/dev/null
  fi
  # подстраховка: сирота ядра (или сама служба), если что-то пошло не по плану
  # раньше шага 6 — cmdline сирот всегда содержит путь изолированного дома.
  pkill -KILL -f "$DOM" 2>/dev/null
}
trap pochistit EXIT

# --- шаг 1: сборка -----------------------------------------------------
mkdir -p "$STEND"
if ! ( cd "$KOREN" && go build -o "$BIN" ./cmd/kelevra ) > "$STEND/build.log" 2>&1; then
  past "сборка" "go build ./cmd/kelevra под linux/$(go env GOARCH 2>/dev/null || echo amd64)" \
    "сборка не прошла" "$(cat "$STEND/build.log")"
fi
shag "сборка" "go build ./cmd/kelevra → $BIN" "$(stat -c%s "$BIN") байт"

# --- шаг 2: изолированный дом -------------------------------------------
if [ ! -f "$YADRO_SRC" ]; then
  past "изолированный дом" "настоящее ядро в $YADRO_SRC" \
    "ядра нет — положи его (sing-box, linux/amd64) в .stend/sing-box-linux ДО этого стенда"
fi
rm -rf "$DOM"
mkdir -p "$DOM/yadro"
cp "$PROFIL" "$DOM/profil.json"
ln -sf "$YADRO_SRC" "$DOM/yadro/sing-box"
if ! "$DOM/yadro/sing-box" version > "$STEND/yadro_version.log" 2>&1; then
  past "изолированный дом" "ядро в $DOM/yadro/sing-box отвечает на version" \
    "бинарь не запускается" "$(cat "$STEND/yadro_version.log")"
fi
shag "изолированный дом" "$DOM: profil.json + yadro/sing-box (симлинк на .stend/sing-box-linux), НЕ ~/.local/share/kelevra" \
  "$(head -1 "$STEND/yadro_version.log")"

# --- шаг 3: своя служба + /api/podklyuchit без прав ----------------------
rm -f "$STEND/sluzhba.log"
# KELEVRA_AVTOREZHIM_DNS="127.0.0.1:1" — см. пояснение в stend/proksi.sh:
# контейнер сам отвечает fake-ip подменой, без неё domaSeychas (#78) честно,
# но ложно решает «дома» и podklyuchit не поднимает защиту.
KELEVRA_DIR="$DOM" KELEVRA_PRAVA=net KELEVRA_BEZ_OBNOVLENIYA=1 \
  KELEVRA_AVTOREZHIM_DNS="127.0.0.1:1" \
  "$BIN" --sluzhba > "$STEND/sluzhba.log" 2>&1 &
SLUZHBA_PID=$!

URL=""
for _ in $(seq 1 40); do
  URL=$(grep -m1 '^KELEVRA-SLUZHBA ' "$STEND/sluzhba.log" 2>/dev/null | awk '{print $2}')
  [ -n "$URL" ] && break
  kill -0 "$SLUZHBA_PID" 2>/dev/null || break
  sleep 0.25
done
if [ -z "$URL" ]; then
  past "своя служба" "HTTP-интерфейс поднялся за 10с (лог $STEND/sluzhba.log)" \
    "адрес службы так и не появился (процесс жив: $(kill -0 "$SLUZHBA_PID" 2>/dev/null && echo да || echo нет))" \
    "$(cat "$STEND/sluzhba.log")"
fi
SOST0=$(curl -s --max-time 5 "${URL}api/sostoyanie") || SOST0=""
if [ -z "$SOST0" ]; then
  past "своя служба" "GET api/sostoyanie отвечает на $URL" "пустой ответ — порт занят под ключом, но не отвечает" ""
fi
shag "своя служба" "процесс pid=$SLUZHBA_PID слушает $URL, api/sostoyanie отвечает: $SOST0" "поднята"

# --- шаг 4: подключение без прав (только прокси) и готовность ядра -------
# Готовность ждём НЕ сном: ручка /api/podklyuchit сама блокируется внутри
# (yadro.Zapustit опрашивает Clash API каждые 300мс, см. internal/yadro/yadro.go)
# и возвращает ответ только когда ядро либо ответило, либо истёк её же таймаут
# (70с). Наш --max-time чуть шире её собственного, а не подменяет ожидание.
PODKL_KOD=$(curl -s -o "$STEND/podklyuchit.json" -w '%{http_code}' --max-time 85 \
  -X POST "${URL}api/podklyuchit") || PODKL_KOD="000"
BEDA=$(pole "$STEND/podklyuchit.json" beda 2>/dev/null || true)
if [ "$PODKL_KOD" != "200" ] || [ -n "$BEDA" ]; then
  past "подключение без прав" "POST api/podklyuchit → 200 {gotovo:true}" \
    "код $PODKL_KOD, беда: ${BEDA:-нет тела}" "$(tail -c 4000 "$DOM/yadro/yadro.log" 2>/dev/null)$(printf '\n---kelevra.log---\n')$(tail -c 2000 "$DOM/kelevra.log" 2>/dev/null)"
fi
SOST1=$(curl -s --max-time 5 "${URL}api/sostoyanie")
SOST_VAL=$(printf '%s' "$SOST1" | pole - sost)
REZHIM_VAL=$(printf '%s' "$SOST1" | pole - rezhim)
YADRO_PID=$(printf '%s' "$SOST1" | pole - pid)
if [ "$SOST_VAL" != "rabotaet" ]; then
  past "подключение без прав" "api/sostoyanie.sost == rabotaet" \
    "sost=$SOST_VAL (ждали rabotaet)" "$(tail -c 4000 "$DOM/yadro/yadro.log" 2>/dev/null)"
fi
if [ "$REZHIM_VAL" = "tunnel" ]; then
  past "подключение без прав" "режим proksi (KELEVRA_PRAVA=net запрещает tunnel)" \
    "получили rezhim=tunnel — KELEVRA_PRAVA не подействовал" ""
fi
shag "подключение без прав" "ядро поднято ЧЕРЕЗ САМО ПРИЛОЖЕНИЕ: pid ядра=$YADRO_PID, режим=$REZHIM_VAL (туннель недостижим в LXC — не пробуем)" "rabotaet"

# --- шаг 5: живой трафик, два независимых канала -------------------------
CFG="$DOM/yadro/config.json"
MIXED_PORT=$(python3 -c '
import json
d = json.load(open("'"$CFG"'"))
for vh in d.get("inbounds", []):
    if vh.get("type") == "mixed":
        print(vh.get("listen_port", ""))
        break
')
CLASH=$(python3 -c '
import json
d = json.load(open("'"$CFG"'"))
c = d.get("experimental", {}).get("clash_api", {})
print(c.get("external_controller", ""), c.get("secret", ""))
')
CLASH_ADRES=$(echo "$CLASH" | awk '{print $1}')
CLASH_SEKRET=$(echo "$CLASH" | awk '{print $2}')
if [ -z "$MIXED_PORT" ] || [ -z "$CLASH_ADRES" ]; then
  past "живой трафик" "прокси-порт и Clash API из рабочего конфига $CFG" \
    "не разобрал конфиг: mixed_port='$MIXED_PORT' clash='$CLASH_ADRES'" "$(cat "$CFG" 2>/dev/null)"
fi

zapros_clash() { # $1 путь Clash API, например /connections
  local hdr=()
  [ -n "$CLASH_SEKRET" ] && hdr=(-H "Authorization: Bearer $CLASH_SEKRET")
  curl -s --max-time 5 "${hdr[@]}" "http://${CLASH_ADRES}$1"
}

DO_TO=$(zapros_clash /connections)
UP0=$(printf '%s' "$DO_TO" | pole - uploadTotal); UP0=${UP0:-0}
VNIZ0=$(printf '%s' "$DO_TO" | pole - downloadTotal); VNIZ0=${VNIZ0:-0}

HTTP_KOD=$(curl -s -o /dev/null -w '%{http_code}' --max-time 15 \
  -x "http://127.0.0.1:${MIXED_PORT}" https://www.gstatic.com/generate_204) || HTTP_KOD="000"

POSLE=$(zapros_clash /connections)
UP1=$(printf '%s' "$POSLE" | pole - uploadTotal); UP1=${UP1:-0}
VNIZ1=$(printf '%s' "$POSLE" | pole - downloadTotal); VNIZ1=${VNIZ1:-0}

if [ "$HTTP_KOD" != "204" ]; then
  past "живой трафик" "curl -x 127.0.0.1:$MIXED_PORT https://www.gstatic.com/generate_204 → 204" \
    "код $HTTP_KOD" "$(tail -c 3000 "$DOM/yadro/yadro.log" 2>/dev/null)"
fi
if ! [ "$UP1" -gt "$UP0" ] || ! [ "$VNIZ1" -gt "$VNIZ0" ]; then
  past "живой трафик" "второй канал: счётчики Clash API (uploadTotal/downloadTotal) выросли" \
    "up $UP0→$UP1, down $VNIZ0→$VNIZ1 — 204 пришёл, но счётчики ядра молчат: канал не прикрыт вторым" ""
fi
shag "живой трафик" "204 через 127.0.0.1:$MIXED_PORT (mixed-in из профиля) И Clash API: up $UP0→$UP1 байт, down $VNIZ0→$VNIZ1 байт" "204 + оба счётчика выросли"

# --- шаг 6: корректное отключение, ядро не остаётся сиротой --------------
OTKL_KOD=$(curl -s -o "$STEND/otklyuchit.json" -w '%{http_code}' --max-time 15 \
  -X POST "${URL}api/otklyuchit") || OTKL_KOD="000"
if [ "$OTKL_KOD" != "200" ]; then
  past "отключение" "POST api/otklyuchit → 200" "код $OTKL_KOD" "$(cat "$STEND/otklyuchit.json" 2>/dev/null)"
fi
sleep 0.5
if [ -n "$YADRO_PID" ] && kill -0 "$YADRO_PID" 2>/dev/null; then
  past "отключение" "процесс ядра pid=$YADRO_PID мёртв после otklyuchit" "процесс всё ещё жив — сирота" ""
fi
kill -TERM "$SLUZHBA_PID" 2>/dev/null
SLUZHBA_MERTVA="нет"
for _ in $(seq 1 20); do
  kill -0 "$SLUZHBA_PID" 2>/dev/null || { SLUZHBA_MERTVA="да"; break; }
  sleep 0.5
done
if [ "$SLUZHBA_MERTVA" != "да" ]; then
  past "отключение" "сама служба (pid=$SLUZHBA_PID) вышла по SIGTERM за 10с" "не вышла" ""
fi
SLUZHBA_PID="" # trap больше не должен её добивать — она уже мертва штатно
if pgrep -f "$DOM" > /dev/null 2>&1; then
  past "отключение" "ни одного процесса с путём $DOM не осталось" \
    "остались: $(pgrep -af "$DOM")" ""
fi
shag "отключение" "otklyuchit → ядро pid=$YADRO_PID мертво, служба вышла по сигналу сама, сирот по $DOM нет" "чисто"

printf '\nВСЁ ЖИВЬЁМ: %d/%d шагов зелёных, отказов ноль.\n' "$SHAG_N" "$VSEGO"
