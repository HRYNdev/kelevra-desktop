#!/usr/bin/env bash
# Стенд «пузырь в трее про новую версию говорит РОВНО ОДИН РАЗ на версию».
#
# Диагноз (26.08, задача про трей): фоновая проверка (internal/sluzhba.
# ProveritObnovlenieFonom) уже кладёт находку в /api/sostoyanie — но человек
# окно не открывает, свернул и забыл. Пузырь в трее (cmd/kelevra/
# trey_windows.go: pokazatOblachkoObnovleniya, подключён из main.go как
# s.OblachkoObnovleniya) — единственный путь, которым уже свёрнутая копия
# может об этом сказать сама. Хозяин продукта в тот же день ругался матом на
# ПОВТОРЯЮЩИЕСЯ уведомления — значит мало показать пузырь, обязательно
# доказать, что он звучит один раз на версию, а не на каждый тик.
#
# На Windows пузырь настоящий (Shell_NotifyIconW/NIM_MODIFY), тут его не
# увидеть без живого explorer.exe — но trey_other.go оставляет для этого
# ЖЕ решения след в журнале, специально ради этого стенда (см. её
# комментарий): «трей (не-Windows заглушка): пузырь про версию X». Стенд
# гоняет настоящий linux-бинарь и настоящую живую службу (--sluzhba),
# судит по числу таких строк в журнале — не по мнению о коде.
#
#   stend/trey_oblachko.sh                  — все сцены, зелёный прогон
#   stend/trey_oblachko.sh --kontrol=a|b|c|d — намеренно ломает ожидание
#     одной сцены (см. past()/KONTROL), чтобы доказать: сцена умеет
#     краснеть, а не просто печатает «зелёный» по привычке.
set -u
KOREN=$(cd "$(dirname "$0")/.." && pwd)
export PATH="$PATH:/usr/local/go/bin"
export HOME=${HOME:-/root}
export GOCACHE=${GOCACHE:-$HOME/.cache/go-build}

STEND="$KOREN/.stend/trey_oblachko"
BIN="$STEND/kelevra_linux"
RELIZY_DOM="$STEND/relizy"
STARAYA_VERSIYA="0.6.20"
VERSIYA_B="0.6.24"   # первая находка (сцены а, б)
VERSIYA_V="0.6.26"   # вышла ещё новее (сцена в)

KONTROL=""
case "${1:-}" in
  --kontrol=a|--kontrol=b|--kontrol=c|--kontrol=d) KONTROL=${1#--kontrol=} ;;
  "") ;;
  *) echo "аргумент не понят: $1 (жду --kontrol=a|b|c|d)" >&2; exit 2 ;;
esac

VSEGO=4
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
pishi_relizy "app-v$VERSIYA_B"
(cd "$RELIZY_DOM" && python3 -m http.server "$PORT" >/dev/null 2>&1 &)
sleep 1

# zapusti_sluzhbu <имя> <KELEVRA_RELIZY> <KELEVRA_PERIOD_OBNOVLENIYA> [KELEVRA_DIR] —
# поднимает отдельную живую копию --sluzhba, возвращает unix-pid через
# PID_SLUZHBY и путь к её журналу через LOG_SLUZHBY. dom по умолчанию свой
# (новый на каждую копию) — сцена «г» ниже нарочно передаёт ТОТ ЖЕ dom
# второй раз, чтобы проверить, переживает ли отметка перезапуск.
zapusti_sluzhbu() {
  local imya=$1 relizy=$2 period=$3 dom=${4:-}
  [ -n "$dom" ] || dom="$STEND/dom_$imya"
  local log="$STEND/sluzhba_$imya.log"
  mkdir -p "$dom"
  rm -f "$log"
  KELEVRA_DIR="$dom" KELEVRA_RELIZY="$relizy" KELEVRA_PERIOD_OBNOVLENIYA="$period" \
    "$BIN" --sluzhba > "$log" 2>&1 &
  PID_SLUZHBY=$!
  PIDY+=("$PID_SLUZHBY")
  LOG_SLUZHBY=$log
  DOM_SLUZHBY=$dom
  local adr=""
  for _ in $(seq 1 40); do
    adr=$(grep -m1 '^KELEVRA-SLUZHBA ' "$log" 2>/dev/null | awk '{print $2}')
    [ -n "$adr" ] && break
    kill -0 "$PID_SLUZHBY" 2>/dev/null || break
    sleep 0.25
  done
  if [ -z "$adr" ]; then
    echo "⚫ ПРИБОР МЁРТВ: копия «$imya» не подняла HTTP за 10с — продукт НЕ проверялся" >&2
    cat "$log" >&2
    pochistit
    exit 7
  fi
}

# schet_puzyrya <лог> <версия> — сколько раз в журнале прозвучал пузырь про
# ЭТУ версию (не-Windows заглушка trey_other.go, см. её комментарий).
schet_puzyrya() {
  grep -cF "трей (не-Windows заглушка): пузырь про версию $2" "$1" 2>/dev/null || true
}

echo "── площадка готова: версия сборки $STARAYA_VERSIYA, поддельный GitHub на 127.0.0.1:$PORT отдаёт app-v$VERSIYA_B ──"

# =============================================================================
# сцена а) фон нашёл новую версию — пузырь прозвучал РОВНО ОДИН РАЗ.
# =============================================================================
[ "$KONTROL" = "a" ] && pishi_relizy "app-v$STARAYA_VERSIYA" # порча: GitHub «отдаёт» ту же версию — объявлять нечего
zapusti_sluzhbu a "http://127.0.0.1:$PORT/relizy.json" "600ms"
LOG_A=$LOG_SLUZHBY; PID_A=$PID_SLUZHBY; DOM_A=$DOM_SLUZHBY
sleep 1.3
[ "$KONTROL" = "a" ] && pishi_relizy "app-v$VERSIYA_B" # откат порчи для следующих сцен, если эта уже провалилась выше
KOL_A=$(schet_puzyrya "$LOG_A" "$VERSIYA_B")
if [ "$KOL_A" != "1" ]; then
  past "нашли новую версию — сказали один раз" "пузырь про $VERSIYA_B в журнале ровно 1 раз" \
    "нашёл $KOL_A раз(а)" "$(cat "$LOG_A")"
fi
shag "нашли новую версию — сказали один раз" "после первого тика пузырь про $VERSIYA_B прозвучал $KOL_A раз" "человек увидит находку, даже свернув окно неделю назад"

# =============================================================================
# сцена б) ещё три тика подряд с ТОЙ ЖЕ версией — пузырь БОЛЬШЕ НЕ звучит.
# Тот же процесс, что и в сцене (а): GitHub всё ещё отдаёт VERSIYA_B.
# =============================================================================
sleep 2.5 # период 600мс — это ещё 3-4 тика поверх сцены (а)
KOL_B=$(schet_puzyrya "$LOG_A" "$VERSIYA_B")
if [ "$KONTROL" = "b" ]; then
  # Здесь нечего испортить в ОКРУЖЕНИИ (GitHub честно отдаёт ту же версию,
  # тик крутится по расписанию как обычно) — сама суть сцены в том, что
  # ничего не должно случиться повторно. Портим требование, как это уже
  # делает --kontrol=d в stend/obnovlenie_fon.sh для «параллельные толчки
  # не роняют процесс»: требуем (неверно), что пузырь ОБЯЗАН повториться.
  if [ "$KOL_B" = "1" ]; then
    past "три тика подряд с той же версией — молчание" "(порча b) требуем повтора, хотя дедупликация обязана его не дать" \
      "как и должно, повтора нет (счёт остался $KOL_B) — сцена поймала бы настоящий повтор, если бы он случился" ""
  fi
fi
if [ "$KOL_B" != "1" ]; then
  past "три тика подряд с той же версией — молчание" "счёт пузырей про $VERSIYA_B остаётся 1 (не растёт)" \
    "стало $KOL_B" "$(cat "$LOG_A")"
fi
shag "три тика подряд с той же версией — молчание" "3+ лишних тика (период 600мс) — счёт остался $KOL_B" "тик не имеет права повторить уже сказанное"
kill -KILL "$PID_A" 2>/dev/null; wait "$PID_A" 2>/dev/null

# =============================================================================
# сцена в) вышла версия ЕЩЁ новее — пузырь про НЕЁ прозвучал снова, один раз.
# Новый процесс на том же $DOM_A (отметка про VERSIYA_B с диска прошлого
# процесса уже там), GitHub переключаем на версию ещё новее ДО старта.
# =============================================================================
if [ "$KONTROL" = "c" ]; then
  echo "  (порча: GitHub НЕ переключен на версию ещё новее — остаётся $VERSIYA_B, объявлять нечего)"
else
  pishi_relizy "app-v$VERSIYA_V"
fi
zapusti_sluzhbu v "http://127.0.0.1:$PORT/relizy.json" "600ms" "$DOM_A" # тот же dom_a: отметка про VERSIYA_B уже на диске
LOG_V=$LOG_SLUZHBY; PID_V=$PID_SLUZHBY
sleep 1.3
KOL_V_STARAYA=$(schet_puzyrya "$LOG_V" "$VERSIYA_B")   # обязана остаться 0 В ЭТОМ журнале — версию B уже сказали в прошлом процессе
KOL_V_NOVAYA=$(schet_puzyrya "$LOG_V" "$VERSIYA_V")
# --kontrol=c не переключил GitHub на версию новее — общая проверка ниже сама
# ловит это (KOL_V_NOVAYA останется 0 вместо ожидаемой 1), отдельная ветка не
# нужна: ровно тот же приём, что у --kontrol=a в сцене (а) выше.
if [ "$KOL_V_STARAYA" != "0" ] || [ "$KOL_V_NOVAYA" != "1" ]; then
  past "версия ещё новее — сказали снова" "$VERSIYA_B молчит (уже сказано раньше), $VERSIYA_V звучит ровно 1 раз" \
    "$VERSIYA_B=$KOL_V_STARAYA раз, $VERSIYA_V=$KOL_V_NOVAYA раз" "$(cat "$LOG_V")"
fi
shag "версия ещё новее — сказали снова" "пузырь про $VERSIYA_V прозвучал $KOL_V_NOVAYA раз, про уже названную $VERSIYA_B — молчание" "каждая новая версия имеет право прозвучать один раз"
kill -KILL "$PID_V" 2>/dev/null; wait "$PID_V" 2>/dev/null

# =============================================================================
# сцена г) перезапуск копии (не только новый тик) — НЕ повод сказать заново
# про уже названную версию. Тот же $DOM_A (отметка на диске уже несёт
# VERSIYA_V из сцены в) поднимается ТРЕТЬИМ процессом; GitHub всё ещё отдаёт
# ту же VERSIYA_V — новой находки нет, и повторный пузырь был бы чистым
# повтором. Это и есть замер решения «на диске, а не в памяти» из
# internal/hranenie.Nastroyki.ObyavlennoeObnovlenie.
# =============================================================================
dom_g="$DOM_A"
[ "$KONTROL" = "d" ] && dom_g="$STEND/dom_d_svezhiy" # порча: СВЕЖИЙ каталог — как будто отметка не пережила рестарт
zapusti_sluzhbu g "http://127.0.0.1:$PORT/relizy.json" "600ms" "$dom_g"
LOG_G=$LOG_SLUZHBY; PID_G=$PID_SLUZHBY
sleep 1.3
KOL_G=$(schet_puzyrya "$LOG_G" "$VERSIYA_V")
# --kontrol=d подсунул свежий каталог (как будто отметка не пережила
# рестарт) — общая проверка ниже сама ловит это (KOL_G станет 1 вместо
# ожидаемого 0), отдельная ветка не нужна: тот же приём, что у --kontrol=a.
if [ "$KOL_G" != "0" ]; then
  past "перезапуск не повод сказать заново" "после перезапуска на том же каталоге пузырь про $VERSIYA_V не звучит снова" \
    "прозвучал $KOL_G раз(а)" "$(cat "$LOG_G")"
fi
shag "перезапуск не повод сказать заново" "новый процесс, тот же каталог данных — пузырь про уже названную $VERSIYA_V молчит" "отметка на диске (hranenie.Nastroyki), а не в памяти процесса — переживает перезапуск"
kill -KILL "$PID_G" 2>/dev/null; wait "$PID_G" 2>/dev/null

if [ -n "$KONTROL" ]; then
  printf '\n(--kontrol=%s должен был провалиться выше — если ты это читаешь, порча не сработала)\n' "$KONTROL"
  exit 1
fi
printf '\nВСЁ ЖИВЬЁМ: %d/%d сцен зелёных. Пузырь в трее называет версию один раз на версию, переживает и тики, и перезапуск.\n' "$SCENA_N" "$VSEGO"
