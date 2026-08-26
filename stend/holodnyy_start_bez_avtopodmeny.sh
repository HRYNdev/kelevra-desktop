#!/usr/bin/env bash
# Стенд «холодный старт больше не подменяет .exe сам».
#
# Заказ хозяина дословно (26.08 11:33): «...приходит обновления сами и ты тыкаешь
# обновление и все, а не автоматом ну и кнопка проверить обновления чтоб
# была». Диагноз: main.go на КАЖДОМ холодном старте звал obnovitsya()
# (cmd/kelevra/obnovlenie.go), который молча скачивал новую сборку, подменял
# .exe на месте и перезапускал приложение — человек это «автоматом» никогда
# не видел, увидеть было нечего: копия на диске уже свежая раньше, чем он
# успевал моргнуть.
#
# Три вещи, которые обязаны быть верны ОДНОВРЕМЕННО после починки:
#   1. холодный старт (--tiho, боевой путь автозапуска Windows) НЕ подменяет
#      .exe на диске — байты те же, что были до запуска;
#   2. при этом обновление НАЙДЕНО и предъявлено человеку (novaya_versiya_
#      dostupna в /api/sostoyanie) за секунды, а не через 4 часа периода;
#   3. тычок в пузырь (POST /api/obnovlenie_postavit) по-прежнему ставит
#      найденное — механизм установки жив, просто требует согласия человека.
#
# Настоящий linux-бинарь, настоящий холодный старт (не --sluzhba напрямую —
# та ветка обновление никогда не проверяла и проверять не должна, см.
# комментарий zapustitSluzhbu), настоящий HTTP путём человека — не юнит-тест
# внутри одного процесса. Файловая семантика Windows (rename поверх занятого
# .exe) тут ни при чём — вопрос не «как проходит замена», а «происходит ли
# она вообще без согласия»; это проверяется байтами файла на любой ОС.
set -u
KOREN=$(cd "$(dirname "$0")/.." && pwd)
export PATH="$PATH:/usr/local/go/bin"
export HOME=${HOME:-/root}
export GOCACHE=${GOCACHE:-$HOME/.cache/go-build}

STEND="$KOREN/.stend/holodnyy_start_bez_avtopodmeny"
DOM_BIN="$STEND/dom_bin/kelevra_linux"   # это же путь исполняется всеми поколениями процессов
RELIZY_DOM="$STEND/relizy"
KELEVRA_DIR="$STEND/dannye"
STARAYA_VERSIYA="0.6.22"
NOVAYA_VERSIYA="0.6.24"

VSEGO=3
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

pole() { # pole <json-файл> <ключ>
  python3 -c '
import json, sys
src, klyuch = sys.argv[1], sys.argv[2]
try:
    d = json.load(open(src))
except Exception:
    print("")
    raise SystemExit
v = d.get(klyuch, "")
print(v if not isinstance(v, bool) else str(v).lower())
' "$1" "$2" 2>/dev/null
}

pochistit() {
  for pid in "${PIDY[@]:-}"; do
    [ -n "$pid" ] && kill -KILL "$pid" 2>/dev/null
  done
  # Три поколения процессов на СТАРОМ коде (исходный → перезапущенный после
  # подмены → служба) исполняют один и тот же путь DOM_BIN — один шаблон
  # ловит всех.
  pkill -KILL -f "$DOM_BIN" 2>/dev/null
  pkill -KILL -f "http.server $PORT" 2>/dev/null
  for pid in "${PIDY[@]:-}"; do
    [ -n "$pid" ] && wait "$pid" 2>/dev/null
  done
}
trap pochistit EXIT

rm -rf "$STEND"
mkdir -p "$(dirname "$DOM_BIN")" "$RELIZY_DOM" "$KELEVRA_DIR"

versiya() { echo "-X github.com/HRYNdev/kelevra-desktop/internal/podpiska.Versiya=$1"; }
if ! ( cd "$KOREN" && go build -ldflags "$(versiya "$STARAYA_VERSIYA")" -o "$DOM_BIN" ./cmd/kelevra ) > "$STEND/build_staraya.log" 2>&1; then
  past "сборка" "go build ./cmd/kelevra (версия $STARAYA_VERSIYA)" "сборка не прошла" "$(cat "$STEND/build_staraya.log")"
fi
if ! ( cd "$KOREN" && go build -ldflags "$(versiya "$NOVAYA_VERSIYA")" -o "$RELIZY_DOM/Kelevra.exe" ./cmd/kelevra ) > "$STEND/build_novaya.log" 2>&1; then
  past "сборка" "go build ./cmd/kelevra (версия $NOVAYA_VERSIYA)" "сборка не прошла" "$(cat "$STEND/build_novaya.log")"
fi

RAZMER=$(stat -c%s "$RELIZY_DOM/Kelevra.exe")
cat > "$RELIZY_DOM/relizy.json" <<JSON
[{"tag_name":"app-v$NOVAYA_VERSIYA","draft":false,"prerelease":false,
  "assets":[{"name":"Kelevra.exe","browser_download_url":"http://127.0.0.1:PORT_PODSTANOVKA/Kelevra.exe","size":$RAZMER}]}]
JSON

PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
sed -i "s/PORT_PODSTANOVKA/$PORT/" "$RELIZY_DOM/relizy.json"
# ПРЯМЫМ ребёнком, не в подоболочке (см. obnovlenie_fon.sh о сиротах, которые
# держат конвейер приёмки): pkill в pochistit ниже целится строкой "$PORT".
python3 -m http.server "$PORT" --directory "$RELIZY_DOM" >/dev/null 2>&1 &
PIDY+=("$!")
sleep 1

echo "── площадка готова: версия сборки $STARAYA_VERSIYA, поддельный GitHub на 127.0.0.1:$PORT отдаёт app-v$NOVAYA_VERSIYA ──"

DO=$(md5sum < "$DOM_BIN")

# =============================================================================
# холодный старт: боевой путь автозапуска Windows (--tiho), НЕ --sluzhba
# напрямую — служба сама обновление никогда не проверяла и не обязана.
# =============================================================================
rm -f "$STEND/kolodnyy.log"
KELEVRA_DIR="$KELEVRA_DIR" KELEVRA_RELIZY="http://127.0.0.1:$PORT/relizy.json" \
  "$DOM_BIN" --tiho > "$STEND/kolodnyy.log" 2>&1 &
PID_HOLODNYY=$!
PIDY+=("$PID_HOLODNYY")

METKA="$KELEVRA_DIR/zapushcheno.json"
NASHLI_METKU=0
for _ in $(seq 1 40); do
  [ -s "$METKA" ] && { NASHLI_METKU=1; break; }
  sleep 0.5
done
if [ "$NASHLI_METKU" -ne 1 ]; then
  echo "⚫ ПРИБОР МЁРТВ: служба не отметилась за 20с — продукт НЕ проверялся" >&2
  cat "$STEND/kolodnyy.log" >&2
  pochistit
  exit 7
fi
ADR=$(pole "$METKA" url)

# --- сцена 1: .exe на диске не подменился ---------------------------------
POSLE=$(md5sum < "$DOM_BIN")
if [ "$DO" != "$POSLE" ]; then
  past ".exe не подменился холодным стартом" "md5 файла на диске до и после холодного старта совпадают" \
    "изменился: до «$DO», после «$POSLE» — приложение само подменило себя без тычка человека" "$(cat "$STEND/kolodnyy.log")"
fi
shag ".exe не подменился холодным стартом" "md5 «$DO» не изменился после --tiho с обновлением на сервере" "автоматом больше не подменяет — человеку есть что тыкать"

# --- сцена 2: обновление найдено и предъявлено за секунды, не за 4 часа ---
NAYDENO=""
for _ in $(seq 1 20); do
  otvet=$(curl -s --max-time 5 "${ADR}api/sostoyanie" 2>/dev/null)
  NAYDENO=$(printf '%s' "$otvet" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("novaya_versiya_dostupna",""))' 2>/dev/null)
  [ "$NAYDENO" = "$NOVAYA_VERSIYA" ] && break
  sleep 0.5
done
if [ "$NAYDENO" != "$NOVAYA_VERSIYA" ]; then
  past "обновление предъявлено за секунды" "novaya_versiya_dostupna == $NOVAYA_VERSIYA в первые ~10с холодного старта" \
    "получил «$NAYDENO»" "$(cat "$STEND/kolodnyy.log")"
fi
shag "обновление предъявлено за секунды" "novaya_versiya_dostupna стало «$NAYDENO» без похода в окно и без ожидания периода" "пузырь заказан сразу на холодном старте, а не через 4 часа"

# --- сцена 3: тычок в пузырь по-прежнему ставит ----------------------------
TYCHOK=$(curl -s -o "$STEND/tychok.json" -w '%{http_code}' --max-time 30 -X POST "${ADR}api/obnovlenie_postavit") || TYCHOK="000"
VERSIYA_POSTAVLENA=$(pole "$STEND/tychok.json" versiya)
if [ "$TYCHOK" != "200" ] || [ "$VERSIYA_POSTAVLENA" != "$NOVAYA_VERSIYA" ]; then
  past "тычок в пузырь ставит" "POST api/obnovlenie_postavit → 200, versiya == $NOVAYA_VERSIYA" \
    "код $TYCHOK, тело $(cat "$STEND/tychok.json" 2>/dev/null)" "$(cat "$STEND/kolodnyy.log")"
fi
shag "тычок в пузырь ставит" "POST api/obnovlenie_postavit поставил версию $VERSIYA_POSTAVLENA по прямому согласию человека" "установка жива — просто больше не сама по себе"

printf '\nВСЁ ЖИВЬЁМ: %d/%d сцен зелёных. Холодный старт больше не подменяет .exe сам, но находка приходит за секунды, а тычок ставит.\n' "$SCENA_N" "$VSEGO"
