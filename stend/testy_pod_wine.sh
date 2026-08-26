#!/usr/bin/env bash
# Гоняет go-тесты продукта СОБРАННЫМИ ПОД WINDOWS и запущенными под wine.
# Зачем: обычная приёмка гоняет тесты нативно на линуксе, где весь windows-путь
# (реестр, WinINet-прокси, окна, служба) подменён заглушками *_other.go и НЕ проверяется.
# Здесь исполняется настоящий windows-код. Это не замена настоящей Windows, но и не ноль.
set -u
KORFN=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
export HOME=${HOME:-/root}
export PATH="$PATH:/usr/local/go/bin"
export GOCACHE=${GOCACHE:-$HOME/.cache/go-build}
WINE=${WINE:-/usr/lib/wine/wine64}
export WINEPREFIX=${WINEPREFIX:-$HOME/.wine}
export WINEDEBUG=${WINEDEBUG:--all}
# Без UTF-8-локали wine не создаёт файлы с кириллицей в имени (см. obshchee.sh).
if [ -z "${LC_ALL:-}" ] || ! echo "${LC_ALL}" | grep -qi 'utf-\?8'; then
  export LC_ALL=C.UTF-8 LANG=C.UTF-8
fi
export XDG_RUNTIME_DIR=${XDG_RUNTIME_DIR:-/tmp/wine-xdg-$(id -u)}
mkdir -p "$XDG_RUNTIME_DIR"
VYHOD=${VYHOD:-/opt/jarvis-goal/repos/.wt}
mkdir -p "$VYHOD"

[ -x "$WINE" ] || { echo "⚫ ПРИБОР МЁРТВ: нет wine ($WINE)"; exit 2; }
cd "$KORFN" || exit 2

PAKETY=$(GOOS=windows go list ./... 2>/dev/null)
[ -n "$PAKETY" ] || { echo "⚫ ПРИБОР МЁРТВ: go list ничего не вернул"; exit 2; }

ZELEN=0; KRASN=0; PUSTO=0; MERTV=0
SPISOK_KRASNYH=""
for P in $PAKETY; do
  IMYA=$(echo "$P" | tr '/' '_')
  KATALOG=$(GOOS=windows go list -f '{{.Dir}}' "$P")
  EXE="$VYHOD/$IMYA.exe"
  if ! GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o "$EXE" "$P" 2>"$VYHOD/$IMYA.sborka"; then
    echo "🔴 $P — НЕ СОБРАЛСЯ под windows"; sed 's/^/    /' "$VYHOD/$IMYA.sborka" | head -5
    KRASN=$((KRASN+1)); SPISOK_KRASNYH="$SPISOK_KRASNYH $P"; continue
  fi
  # пакет без тестов go test -c не создаёт exe
  if [ ! -f "$EXE" ]; then echo "⚪ $P — тестов нет"; PUSTO=$((PUSTO+1)); continue; fi
  LOG="$VYHOD/$IMYA.log"
  ( cd "$KATALOG" && timeout "${TAIMAUT:-300}" "$WINE" "$EXE" -test.v ) >"$LOG" 2>&1
  RC=$?
  if ! grep -qE '^(=== RUN|PASS|FAIL|ok|--- )' "$LOG"; then
    echo "⚫ $P — ПРИБОР МЁРТВ: wine не запустил exe (в логе ни одной строки теста), rc=$RC"
    MERTV=$((MERTV+1)); sed 's/^/    /' "$LOG" | head -3; continue
  fi
  if [ "$RC" -eq 0 ]; then
    N=$(grep -c '^--- PASS' "$LOG")
    PROPUSK=$(grep -c '^--- SKIP' "$LOG")
    # Зелёный поверх пустоты: exe отработал, а не выполнилось НИ ОДНОГО теста —
    # все пропустили себя сами. Это не «прошло», это «не проверялось».
    if [ "$N" -eq 0 ]; then
      echo "🟡 $P — НЕ ПРОВЕРЕНО: 0 выполненных тестов (пропущено $PROPUSK)   лог: $LOG"
      grep -E '^(--- SKIP|    .*\.go:)' "$LOG" | head -6 | sed 's/^/    /'
      KRASN=$((KRASN+1)); SPISOK_KRASNYH="$SPISOK_KRASNYH $P"; continue
    fi
    if [ "$PROPUSK" -gt 0 ]; then
      echo "🟢 $P — $N тест(ов), пропущено $PROPUSK"
    else
      echo "🟢 $P — $N тест(ов)"
    fi
    ZELEN=$((ZELEN+1))
  else
    N=$(grep -c '^--- FAIL' "$LOG")
    echo "🔴 $P — $N упал(о), rc=$RC   лог: $LOG"
    grep -E '^(--- FAIL|    [a-z_]+\.go:)' "$LOG" | head -12 | sed 's/^/    /'
    KRASN=$((KRASN+1)); SPISOK_KRASNYH="$SPISOK_KRASNYH $P"
  fi
done

echo
echo "ИТОГ под wine (windows/amd64): 🟢 $ZELEN  🔴 $KRASN  ⚪ без тестов $PUSTO  ⚫ прибор мёртв $MERTV"
[ -n "$SPISOK_KRASNYH" ] && echo "КРАСНЫЕ:$SPISOK_KRASNYH"
[ "$MERTV" -gt 0 ] && exit 2
[ "$KRASN" -gt 0 ] && exit 1
exit 0
