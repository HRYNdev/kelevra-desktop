#!/usr/bin/env bash
# Стенд «тычок в пузырь = поставилось и перезапустилось» (заказ человека 26.08:
# «просто приходит обновление и ты тыкаешь обновление и всё», не отдельная
# кнопка и не поход в окно).
#
# Диагноз: пузырь в трее (stend/trey_oblachko.sh) уже умеет ЗВАТЬ открыть окно
# и нажать «Проверить обновление» — сама установка по клику отсутствовала.
# Починка — internal/sluzhba.PostavitNaydennoe (тело) и ручка
# /api/obnovlenie_postavit (стык): тычок в пузырь (Windows-часть, не
# проверяемая тут — trey_windows.go) зовёт этот же путь напрямую. Стенд бьёт
# по ручке по HTTP, платформенно-независимо: сердце правки в internal/sluzhba
# нарочно не знает про Windows вообще (см. её комментарий).
#
# Настоящая linux-сборка (две версии, как в stend/obnovlenie_fon.sh), живой
# процесс --sluzhba, поддельный сервер релизов, путь человека по HTTP.
#
#   stend/obnovlenie_postavit.sh                  — все сцены, зелёный прогон
#   stend/obnovlenie_postavit.sh --kontrol=a|b|c|d — намеренно ломает ожидание
#     одной сцены (см. past()/KONTROL), чтобы доказать: сцена умеет
#     краснеть, а не просто печатает «зелёный» по привычке.
set -u
KOREN=$(cd "$(dirname "$0")/.." && pwd)
export PATH="$PATH:/usr/local/go/bin"
export HOME=${HOME:-/root}
export GOCACHE=${GOCACHE:-$HOME/.cache/go-build}

STEND="$KOREN/.stend/obnovlenie_postavit"
RELIZY_DOM="$STEND/relizy"
STARAYA_VERSIYA="0.6.22"
NOVAYA_VERSIYA="0.6.24"
BIN_STARAYA="$STEND/kelevra_staraya"
BIN_NOVAYA="$RELIZY_DOM/Kelevra" # сам файл — и есть «релизная» сборка, отдаём его же по http

KONTROL=""
case "${1:-}" in
  --kontrol=a|--kontrol=b|--kontrol=c|--kontrol=d) KONTROL=${1#--kontrol=} ;;
  "") ;;
  *) echo "аргумент не понят: $1 (жду --kontrol=a|b|c|d)" >&2; exit 2 ;;
esac

VSEGO=4
SCENA_N=0
PIDY=()
PORT=""
HTTP_PID=""

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

pole() { # pole <json-файл-или--> <ключ>
  python3 -c '
import json, sys
src, klyuch = sys.argv[1], sys.argv[2]
try:
    d = json.load(open(src)) if src != "-" else json.load(sys.stdin)
except Exception:
    print("")
    raise SystemExit
v = d.get(klyuch, "")
print(v if not isinstance(v, bool) else str(v).lower())
' "$1" "$2"
}

pishi_relizy() { # pishi_relizy <версия-в-теге> <размер-в-json>
  python3 -c '
import json, sys
teg, port, razmer = sys.argv[1], sys.argv[2], sys.argv[3]
print(json.dumps([{
    "tag_name": teg, "draft": False, "prerelease": False,
    "assets": [{"name": "Kelevra.exe", "browser_download_url": f"http://127.0.0.1:{port}/Kelevra", "size": int(razmer)}],
}]))
' "$1" "$PORT" "$2" > "$RELIZY_DOM/relizy.json"
}

# ubit_i_dozhdatsya <образец для pkill -f> — убить и ДОЖДАТЬСЯ смерти,
# TERM → ждать → KILL, выход только по ТРЁМ подряд пустым замерам (не по
# первому пустому взгляду): боевой путь этой правки порождает копии
# АСИНХРОННО (смена после установки, см. cmd/kelevra: zapustitSmenuPosleObnovleniya
# → podnyatSluzhbuOtdelno), и один взгляд на «пусто» ничего не доказывает
# (горький урок 26.08, PR #57).
ubit_i_dozhdatsya() {
  local obrazec=$1 i pusto=0
  pkill -TERM -f "$obrazec" 2>/dev/null || true
  for i in $(seq 1 20); do
    if pgrep -f "$obrazec" >/dev/null 2>&1; then
      pusto=0
      pkill -KILL -f "$obrazec" 2>/dev/null
    else
      pusto=$((pusto + 1))
      [ "$pusto" -ge 3 ] && return 0
    fi
    sleep 0.5
  done
  echo "⚠ уборка: процессы «$obrazec» пережили TERM и KILL — площадка останется грязной" >&2
  return 1
}

pochistit() {
  for pid in "${PIDY[@]:-}"; do
    [ -n "$pid" ] && kill -KILL "$pid" 2>/dev/null
  done
  ubit_i_dozhdatsya "$STEND/dom_" || true
  if [ -n "$PORT" ]; then
    ubit_i_dozhdatsya "http.server $PORT --directory" || true
  fi
  for pid in "${PIDY[@]:-}"; do
    [ -n "$pid" ] && wait "$pid" 2>/dev/null
  done
}
trap pochistit EXIT

rm -rf "$STEND"
mkdir -p "$STEND" "$RELIZY_DOM"

versiya() { echo "-X github.com/HRYNdev/kelevra-desktop/internal/podpiska.Versiya=$1"; }
if ! ( cd "$KOREN" && go build -ldflags "$(versiya "$STARAYA_VERSIYA")" -o "$BIN_STARAYA" ./cmd/kelevra ) > "$STEND/build_staraya.log" 2>&1; then
  past "сборка" "go build ./cmd/kelevra (версия $STARAYA_VERSIYA)" "сборка не прошла" "$(cat "$STEND/build_staraya.log")"
fi
if ! ( cd "$KOREN" && go build -ldflags "$(versiya "$NOVAYA_VERSIYA")" -o "$BIN_NOVAYA" ./cmd/kelevra ) > "$STEND/build_novaya.log" 2>&1; then
  past "сборка" "go build ./cmd/kelevra (версия $NOVAYA_VERSIYA)" "сборка не прошла" "$(cat "$STEND/build_novaya.log")"
fi
chmod +x "$BIN_STARAYA" "$BIN_NOVAYA"
RAZMER=$(stat -c%s "$BIN_NOVAYA")

PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
pishi_relizy "app-v$NOVAYA_VERSIYA" "$RAZMER"
# Прямой ребёнок с --directory, а не «( ... & )»: pkill -f по подшеллу
# промахивается мимо настоящего python3 (PR #56) — с --directory команда
# несёт путь в самой себе, и pkill -f её видит.
python3 -m http.server "$PORT" --directory "$RELIZY_DOM" > "$STEND/http.log" 2>&1 &
HTTP_PID=$!
PIDY+=("$HTTP_PID")
sleep 1

# zapusti_sluzhbu <имя> — копирует старую сборку в свой изолированный домик
# ($STEND/dom_<имя>/Kelevra — тот самый файл, который PostavitNaydennoe
# заменит на месте) и поднимает её --sluzhba. Возвращает адрес через ADR,
# unix-pid через PID_SLUZHBY, каталог через DOM.
zapusti_sluzhbu() {
  local imya=$1
  # Второй аргумент (любое непустое) — поднять копию БЕЗ немедленной фоновой
  # проверки. С 26.08 sluzhba.SleditZaObnovleniem спрашивает список релизов
  # сразу при старте, а не через период (заказ хозяина: «просто приходит
  # обновление и ты тыкаешь, а не автоматом» — значит находка обязана прийти
  # сама и быстро). Сцене «нечего ставить» нужна копия, которая ещё НИЧЕГО не
  # находила: иначе находка приезжает за доли секунды и postavit отвечает 200
  # совершенно по делу. KELEVRA_BEZ_OBNOVLENIYA гасит только фон и НЕ трогает
  # ручку api/obnovlenie_proverit — поэтому порча b (она эту ручку и дёргает)
  # по-прежнему ловит слепую сцену.
  local bez_fona=${2:-}
  local dom="$STEND/dom_$imya" log="$STEND/sluzhba_$imya.log"
  mkdir -p "$dom"
  cp "$BIN_STARAYA" "$dom/Kelevra"
  chmod +x "$dom/Kelevra"
  rm -f "$log"
  env KELEVRA_DIR="$dom" KELEVRA_RELIZY="http://127.0.0.1:$PORT/relizy.json" \
    ${bez_fona:+KELEVRA_BEZ_OBNOVLENIYA=1} \
    "$dom/Kelevra" --sluzhba > "$log" 2>&1 &
  PID_SLUZHBY=$!
  PIDY+=("$PID_SLUZHBY")
  DOM=$dom
  ADR=""
  for _ in $(seq 1 40); do
    ADR=$(grep -m1 '^KELEVRA-SLUZHBA ' "$log" 2>/dev/null | awk '{print $2}')
    [ -n "$ADR" ] && break
    kill -0 "$PID_SLUZHBY" 2>/dev/null || break
    sleep 0.25
  done
  if [ -z "$ADR" ]; then
    echo "⚫ ПРИБОР МЁРТВ: копия «$imya» не подняла HTTP за 10с — продукт НЕ проверялся" >&2
    cat "$log" >&2
    pochistit
    exit 7
  fi
}

sostoyanie() { curl -s --max-time 5 "${1}api/sostoyanie"; } # sostoyanie <url>
zhiva() { kill -0 "$1" 2>/dev/null; } # zhiva <pid>
naydennoe() { sostoyanie "$1" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("novaya_versiya_dostupna",""))' 2>/dev/null; }
md5f() { md5sum < "$1" 2>/dev/null | awk '{print $1}'; }

echo "── площадка готова: старая $STARAYA_VERSIYA, новая $NOVAYA_VERSIYA (${RAZMER} байт), поддельный GitHub на 127.0.0.1:$PORT ──"

# =============================================================================
# сцена а) ПОСТАВИЛОСЬ И СМЕНИЛОСЬ: тычок ставит найденную сборку на место
# старой и уходит на смену — новая копия поднимается сама, ровно тем же путём,
# каким это уже умеет prava.Poprosit после UAC (cmd/kelevra: --smena, zhdatSmenu).
# =============================================================================
zapusti_sluzhbu a
ADR_A=$ADR; DOM_A=$DOM; PID_A=$PID_SLUZHBY

curl -s -o /dev/null --max-time 5 -X POST "${ADR_A}api/obnovlenie_proverit"
NAYDENO_A=""
for _ in $(seq 1 20); do
  NAYDENO_A=$(naydennoe "$ADR_A")
  [ "$NAYDENO_A" = "$NOVAYA_VERSIYA" ] && break
  sleep 0.25
done
if [ "$NAYDENO_A" != "$NOVAYA_VERSIYA" ]; then
  past "ставится и меняется" "фон нашёл $NOVAYA_VERSIYA до тычка (чтобы было что ставить)" "нашёл «$NAYDENO_A»" "$(cat "$STEND/sluzhba_a.log")"
fi

POSTAVIT_KOD=$(curl -s -o "$STEND/postavit_a.json" -w '%{http_code}' --max-time 10 -X POST "${ADR_A}api/obnovlenie_postavit")
if [ "$POSTAVIT_KOD" != "200" ]; then
  past "ставится и меняется" "POST obnovlenie_postavit → 200" "код $POSTAVIT_KOD" "$(cat "$STEND/postavit_a.json" 2>/dev/null) --- $(cat "$STEND/sluzhba_a.log")"
fi
VERSIYA_OTVETA=$(pole "$STEND/postavit_a.json" versiya)
if [ "$VERSIYA_OTVETA" != "$NOVAYA_VERSIYA" ]; then
  past "ставится и меняется" "ответ versiya == $NOVAYA_VERSIYA" "получил «$VERSIYA_OTVETA»" "$(cat "$STEND/postavit_a.json")"
fi

# Проверяем файл+.old СРАЗУ после 200: обмен файлов идёт синхронно внутри
# самого HTTP-обработчика, а следующая же копия (front-процесс смены) при
# своём холодном старте подчищает .old (obnovitsya→UbratHvost) — если
# опоздать с проверкой, .old может уже исчезнуть не по вине этой правки.
NOVYY_MD5=$(md5f "$DOM_A/Kelevra"); RELIZ_MD5=$(md5f "$BIN_NOVAYA")
STARYY_MD5_FAKT=$(md5f "$DOM_A/Kelevra.old"); STARYY_MD5_OZHID=$(md5f "$BIN_STARAYA")
if [ "$KONTROL" = "a" ]; then
  # порча: требуем (неверно), что файл на диске НЕ поменялся.
  if [ "$NOVYY_MD5" != "$RELIZ_MD5" ]; then
    past "ставится и меняется" "(порча a) требуем, что файл НЕ обновился" "и вправду не обновился — порча не сработала, перепроверь сцену" ""
  else
    past "ставится и меняется" "(порча a) требуем, что файл НЕ обновился" "файл всё-таки обновился, как и должно — сцена поймала подмену" ""
  fi
fi
if [ "$NOVYY_MD5" != "$RELIZ_MD5" ]; then
  past "ставится и меняется" "файл на диске стал НОВОЙ сборкой (md5 совпал с релизом)" "md5 не совпал: файл=$NOVYY_MD5 релиз=$RELIZ_MD5" ""
fi
if [ "$STARYY_MD5_FAKT" != "$STARYY_MD5_OZHID" ]; then
  past "ставится и меняется" "рядом лежит хвост .old со СТАРОЙ сборкой" "md5 не совпал или .old нет: факт=$STARYY_MD5_FAKT ожидали=$STARYY_MD5_OZHID" ""
fi

STARAYA_UMERLA=1
for _ in $(seq 1 40); do
  if ! zhiva "$PID_A"; then STARAYA_UMERLA=0; break; fi
  sleep 0.25
done
if [ "$STARAYA_UMERLA" -ne 0 ]; then
  past "ставится и меняется" "старая копия (pid=$PID_A) завершилась после установки" "процесс жив спустя 10с" "$(cat "$STEND/sluzhba_a.log")"
fi

NOVYY_ADR="" NOVAYA_ZHIVA_VERSIYA="" NOVYY_PID=""
for _ in $(seq 1 60); do
  NOVYY_ADR=$(pole "$DOM_A/zapushcheno.json" url 2>/dev/null)
  if [ -n "$NOVYY_ADR" ] && [ "$NOVYY_ADR" != "$ADR_A" ]; then
    NOVAYA_ZHIVA_VERSIYA=$(sostoyanie "$NOVYY_ADR" 2>/dev/null | python3 -c 'import json,sys; print(json.load(sys.stdin).get("versiya",""))' 2>/dev/null)
    [ "$NOVAYA_ZHIVA_VERSIYA" = "$NOVAYA_VERSIYA" ] && break
  fi
  sleep 0.25
done
if [ "$NOVAYA_ZHIVA_VERSIYA" != "$NOVAYA_VERSIYA" ]; then
  past "ставится и меняется" "поднялась НОВАЯ копия версии $NOVAYA_VERSIYA (метка сменилась и отвечает)" "получил версию «$NOVAYA_ZHIVA_VERSIYA» по адресу «$NOVYY_ADR»" "$(cat "$STEND/sluzhba_a.log")"
fi
NOVYY_PID=$(pole "$DOM_A/zapushcheno.json" pid 2>/dev/null)
shag "ставится и меняется" "postavit→200 (версия $VERSIYA_OTVETA), файл+.old на месте, старая копия умерла, новая отвечает версией $NOVAYA_ZHIVA_VERSIYA" "тычок в пузырь = поставилось и перезапустилось"
ubit_i_dozhdatsya "$DOM_A/Kelevra" || true

# =============================================================================
# сцена б) НЕЧЕГО СТАВИТЬ: без предварительной проверки ставить нечего —
# 400, процесс жив, ничего не тронуто.
# =============================================================================
zapusti_sluzhbu b bez_fona
ADR_B=$ADR; PID_B=$PID_SLUZHBY
if [ "$KONTROL" = "b" ]; then
  # порча: спрашиваем GitHub заранее (аномально для этой сцены) — теперь
  # обновление НАЙДЕНО, но всё равно (неверно) ждём 400 ниже.
  curl -s -o /dev/null --max-time 5 -X POST "${ADR_B}api/obnovlenie_proverit"
  for _ in $(seq 1 20); do
    [ "$(naydennoe "$ADR_B")" = "$NOVAYA_VERSIYA" ] && break
    sleep 0.25
  done
fi
POSTAVIT_KOD_B=$(curl -s -o "$STEND/postavit_b.json" -w '%{http_code}' --max-time 10 -X POST "${ADR_B}api/obnovlenie_postavit")
if [ "$KONTROL" = "b" ]; then
  if [ "$POSTAVIT_KOD_B" = "400" ]; then
    past "нечего ставить без проверки" "(порча b) требуем 400 после того, как проверка уже нашла версию" "и вправду 400 — порча не сработала, перепроверь сцену" "$(cat "$STEND/postavit_b.json")"
  else
    past "нечего ставить без проверки" "(порча b) требуем 400, хотя проверка уже нашла версию" "получил код $POSTAVIT_KOD_B (по делу — не 400) — сцена поймала подмену" ""
  fi
fi
if [ "$POSTAVIT_KOD_B" != "400" ]; then
  past "нечего ставить без проверки" "без предварительной проверки POST obnovlenie_postavit → 400" "код $POSTAVIT_KOD_B" "$(cat "$STEND/postavit_b.json")"
fi
if ! zhiva "$PID_B"; then
  past "нечего ставить без проверки" "процесс (pid=$PID_B) жив после отказа" "процесс умер" "$(cat "$STEND/sluzhba_b.log")"
fi
shag "нечего ставить без проверки" "POST obnovlenie_postavit без предварительной проверки → 400, процесс жив" "нечем скачивать — находки не было"
kill -KILL "$PID_B" 2>/dev/null; wait "$PID_B" 2>/dev/null

# =============================================================================
# сцена в) НЕ ПОСТАВИЛОСЬ — НЕ УМИРАЕМ: релиз объявляет размер, которому
# скачанные байты не соответствуют (битая/урезанная сборка) — Postavit()
# ловит это сверкой С ФАКТОМ на диске и отказывается: 400, процесс жив,
# файл на месте не тронут.
# =============================================================================
pishi_relizy "app-v$NOVAYA_VERSIYA" "$((RAZMER + 1))" # порча JSON: размер на 1 байт больше настоящего
[ "$KONTROL" = "c" ] && pishi_relizy "app-v$NOVAYA_VERSIYA" "$RAZMER" # (порча стенда c) — размер на самом деле верный
zapusti_sluzhbu v
ADR_V=$ADR; DOM_V=$DOM; PID_V=$PID_SLUZHBY
curl -s -o /dev/null --max-time 5 -X POST "${ADR_V}api/obnovlenie_proverit"
NAYDENO_V=""
for _ in $(seq 1 20); do
  NAYDENO_V=$(naydennoe "$ADR_V")
  [ "$NAYDENO_V" = "$NOVAYA_VERSIYA" ] && break
  sleep 0.25
done
if [ "$NAYDENO_V" != "$NOVAYA_VERSIYA" ]; then
  past "битая сборка не убивает" "проверка нашла $NOVAYA_VERSIYA (чтобы было что ставить)" "нашла «$NAYDENO_V»" "$(cat "$STEND/sluzhba_v.log")"
fi
POSTAVIT_KOD_V=$(curl -s -o "$STEND/postavit_v.json" -w '%{http_code}' --max-time 15 -X POST "${ADR_V}api/obnovlenie_postavit")
if [ "$KONTROL" = "c" ]; then
  if [ "$POSTAVIT_KOD_V" = "400" ]; then
    past "битая сборка не убивает" "(порча c) при верном размере (рабочая сборка) требуем 400" "и вправду 400 — порча не сработала, перепроверь сцену" "$(cat "$STEND/postavit_v.json")"
  else
    past "битая сборка не убивает" "(порча c) требуем 400 при рабочей сборке" "получил $POSTAVIT_KOD_V (по делу — 200) — сцена поймала подмену" ""
  fi
fi
if [ "$POSTAVIT_KOD_V" != "400" ]; then
  past "битая сборка не убивает" "POST obnovlenie_postavit при испорченном размере → 400" "код $POSTAVIT_KOD_V" "$(cat "$STEND/postavit_v.json") --- $(cat "$STEND/sluzhba_v.log")"
fi
if ! zhiva "$PID_V"; then
  past "битая сборка не убивает" "процесс (pid=$PID_V) жив после неудачной установки" "процесс умер" "$(cat "$STEND/sluzhba_v.log")"
fi
if [ "$(md5f "$DOM_V/Kelevra")" != "$(md5f "$BIN_STARAYA")" ]; then
  past "битая сборка не убивает" "файл на диске НЕ тронут (остался $STARAYA_VERSIYA)" "md5 изменился, хотя установка провалилась" ""
fi
shag "битая сборка не убивает" "POST obnovlenie_postavit → 400, процесс жив, файл на диске не тронут" "неудачная установка не роняет и не портит рабочую копию"
kill -KILL "$PID_V" 2>/dev/null; wait "$PID_V" 2>/dev/null

# =============================================================================
# сцена г) ДВА ТЫЧКА: два POST подряд (почти одновременно) не приводят к
# двум скачиваниям — idetUstanovkaObnovleniya держит замок так же, как
# idetProverkaObnovleniya держит его у фоновой проверки (stend/obnovlenie_fon.sh).
# =============================================================================
pishi_relizy "app-v$NOVAYA_VERSIYA" "$RAZMER" # возвращаем верный размер после сцены в)
zapusti_sluzhbu g
ADR_G=$ADR; DOM_G=$DOM; PID_G=$PID_SLUZHBY
curl -s -o /dev/null --max-time 5 -X POST "${ADR_G}api/obnovlenie_proverit"
for _ in $(seq 1 20); do
  [ "$(naydennoe "$ADR_G")" = "$NOVAYA_VERSIYA" ] && break
  sleep 0.25
done

DO_G=$(grep -c 'GET /Kelevra ' "$STEND/http.log" 2>/dev/null); DO_G=${DO_G:-0}
curl -s -o "$STEND/tychok1.json" -w '%{http_code}' --max-time 15 -X POST "${ADR_G}api/obnovlenie_postavit" > "$STEND/kod1.txt" &
P1=$!
curl -s -o "$STEND/tychok2.json" -w '%{http_code}' --max-time 15 -X POST "${ADR_G}api/obnovlenie_postavit" > "$STEND/kod2.txt" &
P2=$!
wait "$P1" 2>/dev/null
wait "$P2" 2>/dev/null
sleep 2
POSLE_G=$(grep -c 'GET /Kelevra ' "$STEND/http.log" 2>/dev/null); POSLE_G=${POSLE_G:-0}
SKACHANO_G=$((POSLE_G - DO_G))
KOD1=$(cat "$STEND/kod1.txt" 2>/dev/null); KOD2=$(cat "$STEND/kod2.txt" 2>/dev/null)
KODOV_200=0
[ "$KOD1" = "200" ] && KODOV_200=$((KODOV_200 + 1))
[ "$KOD2" = "200" ] && KODOV_200=$((KODOV_200 + 1))

if [ "$KONTROL" = "d" ]; then
  # порча: требуем (неверно) ровно 2 скачивания вместо одного.
  if [ "$SKACHANO_G" -eq 2 ]; then
    past "два тычка не качают дважды" "(порча d) требуем (неверно) 2 скачивания" "и вправду 2 — порча не сработала, перепроверь сцену" ""
  else
    past "два тычка не качают дважды" "(порча d) требуем 2 скачивания" "получил $SKACHANO_G — сцена поймала подмену (замок держит)" ""
  fi
fi
if [ "$SKACHANO_G" -ne 1 ]; then
  past "два тычка не качают дважды" "ровно ОДНО обращение к поддельному серверу за файлом" "получил $SKACHANO_G обращений (коды тычков: $KOD1, $KOD2)" "$(cat "$STEND/sluzhba_g.log")"
fi
if [ "$KODOV_200" -ne 1 ]; then
  past "два тычка не качают дважды" "ровно один из двух тычков вернул 200 (второй — «установка уже идёт»)" "коды: $KOD1 и $KOD2" ""
fi
shag "два тычка не качают дважды" "коды тычков $KOD1/$KOD2, скачиваний у поддельного сервера: $SKACHANO_G" "idetUstanovkaObnovleniya держит гонку — та же схема, что и у фоновой проверки"
ubit_i_dozhdatsya "$DOM_G/Kelevra" || true

if [ -n "$KONTROL" ]; then
  printf '\n(--kontrol=%s должен был провалиться выше — если ты это читаешь, порча не сработала)\n' "$KONTROL"
  exit 1
fi
printf '\nВСЁ ЖИВЬЁМ: %d/%d сцен зелёных. Тычок в пузырь ставит найденное обновление и уходит на смену — заказ человека 26.08 закрыт.\n' "$SCENA_N" "$VSEGO"
