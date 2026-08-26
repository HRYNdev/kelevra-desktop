#!/usr/bin/env bash
# Стенд «узнал о новой версии, не закрывая Kelevra».
#
# Диагноз (26.08): механизм обновления сам по себе исправен, но звучит РОВНО
# ОДИН РАЗ — на холодном старте (obnovitsya(), cmd/kelevra/main.go). Копия,
# которую человек не закрывал днями (обычный режим — свернул в трей и
# забыл), эту проверку больше не проходит никогда: 0.6.23 и 0.6.24 у него
# на руках разошлись 0 раз именно поэтому.
#
# Починка тут — не про сборку .exe (там ничего Windows-специфичного, файловая
# семантика Postavit() уже покрыта stend/obnovlenie.sh под wine): она про то,
# ЧТО и КОГДА уже работающая копия сама спрашивает GitHub, и про то, что
# находка обязана попасть человеку на глаза тем же путём, каким видна
# остальная правда о приложении — /api/sostoyanie (index.html читает поле
# novaya_versiya_dostupna той же функцией podpisObnovleniya, что и ручную
# кнопку «Проверить обновление»). Настоящая linux-сборка, живой процесс
# --sluzhba, HTTP путём человека — не юнит-тест внутри одного процесса.
#
#   stend/obnovlenie_fon.sh                  — все сцены, зелёный прогон
#   stend/obnovlenie_fon.sh --kontrol=a|b|c|d — намеренно ломает ожидание
#     одной сцены (см. past()/KONTROL), чтобы доказать: сцена умеет
#     краснеть, а не просто печатает «зелёный» по привычке.
set -u
KOREN=$(cd "$(dirname "$0")/.." && pwd)
export PATH="$PATH:/usr/local/go/bin"
export HOME=${HOME:-/root}
export GOCACHE=${GOCACHE:-$HOME/.cache/go-build}

STEND="$KOREN/.stend/obnovlenie_fon"
BIN="$STEND/kelevra_linux"
RELIZY_DOM="$STEND/relizy"
STARAYA_VERSIYA="0.6.22"
NOVAYA_VERSIYA="0.6.24"

KONTROL=""
case "${1:-}" in
  --kontrol=a|--kontrol=b|--kontrol=c|--kontrol=d) KONTROL=${1#--kontrol=} ;;
  "") ;;
  *) echo "аргумент не понят: $1 (жду --kontrol=a|b|c|d)" >&2; exit 2 ;;
esac

VSEGO=4
SCENA_N=0
# pid-ы фоновых процессов, которые trap обязан прибить на любом выходе.
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

pishi_relizy() { # pishi_relizy <версия-в-теге> — переписывает relizy.json без перезапуска http.server
  python3 -c '
import json, sys
teg, port = sys.argv[1], sys.argv[2]
print(json.dumps([{
    "tag_name": teg, "draft": False, "prerelease": False,
    "assets": [{"name": "Kelevra.exe", "browser_download_url": f"http://127.0.0.1:{port}/Kelevra.exe", "size": 42}],
}]))
' "$1" "$PORT" > "$RELIZY_DOM/relizy.json"
}

pochistit() {
  for pid in "${PIDY[@]:-}"; do
    [ -n "$pid" ] && kill -KILL "$pid" 2>/dev/null
  done
  pkill -KILL -f "$STEND" 2>/dev/null
}
trap pochistit EXIT

rm -rf "$STEND"
mkdir -p "$STEND" "$RELIZY_DOM"
if ! ( cd "$KOREN" && go build -ldflags "-X github.com/HRYNdev/kelevra-desktop/internal/podpiska.Versiya=$STARAYA_VERSIYA" -o "$BIN" ./cmd/kelevra ) > "$STEND/build.log" 2>&1; then
  past "сборка" "go build ./cmd/kelevra (версия $STARAYA_VERSIYA)" "сборка не прошла" "$(cat "$STEND/build.log")"
fi

PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
pishi_relizy "app-v$NOVAYA_VERSIYA"
# ПРЯМЫМ ребёнком, не в подоболочке. `( … & )` теряет pid внутри подоболочки,
# в PIDY он не попадает, а pkill в pochistit по нему промахивается: в строке
# `python3 -m http.server <порт>` НЕТ ни пути стенда, ни чего-либо нашего.
# 26.08 из-за этого сервер пережил стенд и ДЕРЖАЛ ОТКРЫТЫМ конвейер всей
# приёмки: vse.sh печатал итог, а вызвавший его vypusk.sh не получал EOF и
# висел вечно — два выпуска подряд были сняты по таймауту как «долгие».
python3 -m http.server "$PORT" --directory "$RELIZY_DOM" >/dev/null 2>&1 &
PIDY+=("$!")
sleep 1

# zapusti_sluzhbu <имя> <KELEVRA_RELIZY> <KELEVRA_PERIOD_OBNOVLENIYA> — поднимает
# отдельную живую копию --sluzhba (боевой служебный режим — тот же, что
# держит трей и API человека), возвращает URL с ключом через глобальную ADR
# и unix-pid через PID_SLUZHBY.
zapusti_sluzhbu() {
  local imya=$1 relizy=$2 period=$3
  local dom="$STEND/dom_$imya" log="$STEND/sluzhba_$imya.log"
  mkdir -p "$dom"
  rm -f "$log"
  KELEVRA_DIR="$dom" KELEVRA_RELIZY="$relizy" KELEVRA_PERIOD_OBNOVLENIYA="$period" \
    "$BIN" --sluzhba > "$log" 2>&1 &
  PID_SLUZHBY=$!
  PIDY+=("$PID_SLUZHBY")
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

echo "── площадка готова: версия сборки $STARAYA_VERSIYA, поддельный GitHub на 127.0.0.1:$PORT отдаёт app-v$NOVAYA_VERSIYA ──"

# =============================================================================
# сцена а) БЕЗ единого клика человека и БЕЗ ожидания тика находит обновление и
# кладёт его в /api/sostoyanie — тем же полем, каким index.html показывает
# ручную кнопку «Проверить обновление» (podpisObnovleniya, oblik/index.html).
# Первая проверка — СРАЗУ при старте (заказ хозяина 26.08: «приходит само»), не
# ждёт периода тикера: период тут нарочно огромный (1ч), чтобы доказать —
# находка пришла от немедленной первой проверки, а не от совпавшего тика.
# =============================================================================
[ "$KONTROL" = "a" ] && pishi_relizy "app-v$STARAYA_VERSIYA" # порча: GitHub «отдаёт» ту же версию — обновляться не на что
zapusti_sluzhbu a "http://127.0.0.1:$PORT/relizy.json" "1h"
ADR_A=$ADR; PID_A=$PID_SLUZHBY
NAYDENO_A=""
for _ in $(seq 1 20); do
  NAYDENO_A=$(sostoyanie "$ADR_A" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("novaya_versiya_dostupna",""))' 2>/dev/null)
  [ "$NAYDENO_A" = "$NOVAYA_VERSIYA" ] && break
  sleep 0.25
done
[ "$KONTROL" = "a" ] && pishi_relizy "app-v$NOVAYA_VERSIYA" # откат порчи для следующих сцен, если сцена уже провалилась выше
if [ "$NAYDENO_A" != "$NOVAYA_VERSIYA" ]; then
  past "фон находит обновление сразу при старте" "novaya_versiya_dostupna == $NOVAYA_VERSIYA в первые секунды (период сам — 1ч)" \
    "получил «$NAYDENO_A»" "$(cat "$STEND/sluzhba_a.log")"
fi
shag "фон находит обновление сразу при старте" "novaya_versiya_dostupna стало «$NAYDENO_A» без ожидания тика (период 1ч)" "человек увидит находку в первые секунды, даже ничего не нажимая"
kill -KILL "$PID_A" 2>/dev/null; wait "$PID_A" 2>/dev/null

# =============================================================================
# сцена б) открытие окна уже работающей копии (main.go: chuzhaya →
# podtolknutFonovuyuProverku → POST /api/obnovlenie_proverit) даёт ответ
# сразу, не дожидаясь тика — период тут нарочно огромный (1ч), чтобы
# доказать: находка пришла ИМЕННО от толчка, а не от совпавшего тика.
# =============================================================================
zapusti_sluzhbu b "http://127.0.0.1:$PORT/relizy.json" "1h"
ADR_B=$ADR; PID_B=$PID_SLUZHBY
if [ "$KONTROL" != "b" ]; then
  TOLCHOK_KOD=$(curl -s -o "$STEND/tolchok_b.json" -w '%{http_code}' --max-time 5 -X POST "${ADR_B}api/obnovlenie_proverit") || TOLCHOK_KOD="000"
  if [ "$TOLCHOK_KOD" != "200" ]; then
    past "открытие окна чужой копии толкает проверку" "POST api/obnovlenie_proverit → 200" "код $TOLCHOK_KOD" "$(cat "$STEND/tolchok_b.json" 2>/dev/null)"
  fi
fi
[ "$KONTROL" = "b" ] && echo "  (порча: толчок нарочно НЕ послан — период 1ч сам себя не догонит за время сцены)"
NAYDENO_B=""
for _ in $(seq 1 20); do
  NAYDENO_B=$(sostoyanie "$ADR_B" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("novaya_versiya_dostupna",""))' 2>/dev/null)
  [ "$NAYDENO_B" = "$NOVAYA_VERSIYA" ] && break
  sleep 0.25
done
if [ "$NAYDENO_B" != "$NOVAYA_VERSIYA" ]; then
  past "открытие окна чужой копии толкает проверку" "novaya_versiya_dostupna == $NOVAYA_VERSIYA в течение 5с после толчка (период сам — 1ч)" \
    "получил «$NAYDENO_B»" "$(cat "$STEND/sluzhba_b.log")"
fi
shag "открытие окна чужой копии толкает проверку" "толчок POST obnovlenie_proverit → находка «$NAYDENO_B» за секунды, а не за час периода" "клик по трею — тоже повод спросить"
kill -KILL "$PID_B" 2>/dev/null; wait "$PID_B" 2>/dev/null

# =============================================================================
# сцена в) сеть недоступна (GitHub не отвечает) — фоновая проверка обязана
# пережить это тихо: процесс жив, /api/sostoyanie отвечает как ни в чём не
# бывало, находка не появляется мусором.
# =============================================================================
MERTVYY_ADRES="http://127.0.0.1:1/nikogo-tam-net"
RELIZY_V="$MERTVYY_ADRES"
[ "$KONTROL" = "c" ] && RELIZY_V="http://127.0.0.1:$PORT/relizy.json" # порча: сеть на самом деле ЕСТЬ и отвечает
zapusti_sluzhbu v "$RELIZY_V" "1h"
ADR_V=$ADR; PID_V=$PID_SLUZHBY
curl -s -o /dev/null --max-time 5 -X POST "${ADR_V}api/obnovlenie_proverit"
sleep 1.5
if ! zhiva "$PID_V"; then
  past "недоступная сеть не роняет процесс" "процесс (pid=$PID_V) жив после проверки на мёртвом адресе" "процесс умер" "$(cat "$STEND/sluzhba_v.log")"
fi
SOST_V=$(sostoyanie "$ADR_V")
SOST_V_KOD=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "${ADR_V}api/sostoyanie")
NAYDENO_V=$(printf '%s' "$SOST_V" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("novaya_versiya_dostupna",""))' 2>/dev/null)
if [ "$KONTROL" = "c" ]; then
  # порча: сеть на самом деле рабочая — требуем (неверно) что поле ПУСТОЕ,
  # хотя оно обязано заполниться, раз GitHub ответил правду.
  if [ -z "$NAYDENO_V" ]; then
    past "недоступная сеть не роняет процесс" "(порча c) при РАБОЧЕЙ сети ожидали пустое поле" "и вправду пусто — порча не сработала, перепроверь сцену" ""
  else
    past "недоступная сеть не роняет процесс" "(порча c) поле обязано остаться пустым" "заполнилось «$NAYDENO_V», как и должно при рабочей сети — сцена поймала подмену" ""
  fi
fi
if [ "$SOST_V_KOD" != "200" ] || [ -n "$NAYDENO_V" ]; then
  past "недоступная сеть не роняет процесс" "api/sostoyanie → 200, novaya_versiya_dostupna осталось пустым" \
    "код $SOST_V_KOD, поле «$NAYDENO_V»" "$(cat "$STEND/sluzhba_v.log")"
fi
if ! grep -q "фоновая проверка обновления: не вышло" "$STEND/sluzhba_v.log"; then
  past "недоступная сеть не роняет процесс" "в журнале — спокойная запись об отказе сети (не паника)" "записи не нашлось" "$(cat "$STEND/sluzhba_v.log")"
fi
shag "недоступная сеть не роняет процесс" "pid=$PID_V жив, api/sostoyanie снова 200, поле пустое, в журнале спокойная запись" "тихий отказ, а не авария"
kill -KILL "$PID_V" 2>/dev/null; wait "$PID_V" 2>/dev/null

# =============================================================================
# сцена г) несколько «кликов по трею» разом (несколько толчков параллельно)
# не наслаиваются друг на друга и не роняют процесс — idetProverkaObnovleniya
# держит замок, а итоговое состояние остаётся ОДНОЙ читаемой версией, не
# мусором от гонки.
# =============================================================================
zapusti_sluzhbu g "http://127.0.0.1:$PORT/relizy.json" "1h"
ADR_G=$ADR; PID_G=$PID_SLUZHBY
TOLCHKI_PIDY=()
for _ in 1 2 3 4 5 6 7 8; do
  curl -s -o /dev/null --max-time 5 -X POST "${ADR_G}api/obnovlenie_proverit" &
  TOLCHKI_PIDY+=("$!")
done
# wait по КОНКРЕТНЫМ pid-ам толчков, а не голый wait: тот ждёт ВСЕ фоновые
# задания шелла разом, включая уже убитые (kill -KILL) процессы предыдущих
# сцен, ещё не собранные явным wait, — на этой машине голый wait наблюдаемо
# подвисал именно на них, а не на самих curl (замерено: 8 параллельных curl
# к живой службе сами по себе укладываются в 0.04с).
for pid in "${TOLCHKI_PIDY[@]}"; do
  wait "$pid" 2>/dev/null
done
sleep 1
if [ "$KONTROL" = "d" ]; then
  # порча: требуем (неверно), что процесс НЕ пережил параллельный толчок.
  if zhiva "$PID_G"; then
    past "параллельные толчки не наслаиваются" "(порча d) процесс обязан был умереть" "жив как ни в чём не бывало — сцена поймала подмену" ""
  fi
fi
if ! zhiva "$PID_G"; then
  past "параллельные толчки не наслаиваются" "процесс (pid=$PID_G) жив после 8 одновременных толчков" "процесс умер" "$(cat "$STEND/sluzhba_g.log")"
fi
SOST_G_KOD=$(curl -s -o "$STEND/sost_g.json" -w '%{http_code}' --max-time 5 "${ADR_G}api/sostoyanie")
NAYDENO_G=$(pole "$STEND/sost_g.json" novaya_versiya_dostupna)
if [ "$SOST_G_KOD" != "200" ] || [ "$NAYDENO_G" != "$NOVAYA_VERSIYA" ]; then
  past "параллельные толчки не наслаиваются" "api/sostoyanie → 200, novaya_versiya_dostupna == $NOVAYA_VERSIYA (одна связная находка, не мусор)" \
    "код $SOST_G_KOD, поле «$NAYDENO_G»" "$(cat "$STEND/sluzhba_g.log")"
fi
shag "параллельные толчки не наслаиваются" "8 одновременных POST obnovlenie_proverit — процесс жив, итог один и тот же: «$NAYDENO_G»" "замок idetProverkaObnovleniya держит гонку"
kill -KILL "$PID_G" 2>/dev/null; wait "$PID_G" 2>/dev/null

if [ -n "$KONTROL" ]; then
  printf '\n(--kontrol=%s должен был провалиться выше — если ты это читаешь, порча не сработала)\n' "$KONTROL"
  exit 1
fi
printf '\nВСЁ ЖИВЬЁМ: %d/%d сцен зелёных. Уже работающая копия сама узнаёт о новой версии и показывает это тем же полем, что и ручная кнопка.\n' "$SCENA_N" "$VSEGO"
