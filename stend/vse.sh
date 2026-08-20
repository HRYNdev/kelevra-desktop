#!/usr/bin/env bash
# Одна дверь в приёмку: собрать, прогнать тесты и КАЖДЫЙ стенд из папки.
#
# Зачем именно так. Стенды множились по одному файлу на беду, а запускал их я
# руками, по памяти. 20.08 выяснилось, что два из них (avtozapusk, obnovlenie)
# не гонялись НИГДЕ, а я в дневнике хвалился их зеленью. Поэтому список стендов
# берётся с диска глобом, а не пишется руками: новый файл в stend/ попадает в
# приёмку сам, без моего участия и без записи в чей-нибудь список.
#
# Что пропущено — печатаем вслух. Молчаливый пропуск читается как «прогнали
# всё», хотя прогнали половину.
#
#   stend/vse.sh            — всё, что можно на этой машине
#   BEZ_WINE=1 stend/vse.sh — без стендов, которым нужен wine
set -u
KOREN=$(cd "$(dirname "$0")/.." && pwd)
export PATH="$PATH:/usr/local/go/bin"
export HOME=${HOME:-/root}
export GOCACHE=${GOCACHE:-$HOME/.cache/go-build}
TAYMAUT=${TAYMAUT:-900}

# Не стенды, а подсобка: их запуск ничего не проверяет.
# Список короткий и растёт медленно; главное — по умолчанию файл СЧИТАЕТСЯ
# стендом, то есть новый щуп попадает в приёмку сам, а не забывается.
NE_STEND=("vse.sh" "znachok.py")
# Нужен wine: под ним гоняются настоящие windows-бинарники.
NUZHEN_WINE=("windows.sh" "razdvoenie.sh" "trey.sh" "obnovlenie.sh" "proksi.sh")

est_v() { local i n=$1; shift; for i in "$@"; do [ "$i" = "$n" ] && return 0; done; return 1; }

ZELENYE=(); KRASNYE=(); PROPUSK=()

shag() {  # shag <имя> <команда...>
  local imya=$1; shift
  printf '\n═══ %s\n' "$imya"
  if timeout "$TAYMAUT" "$@"; then
    ZELENYE+=("$imya")
  else
    local rc=$?
    printf '   ✗ %s: rc=%d\n' "$imya" "$rc"
    KRASNYE+=("$imya (rc=$rc)")
  fi
}

cd "$KOREN" || exit 1
shag "go build" go build ./...
shag "go test" go test ./...

for put in "$KOREN"/stend/*.sh "$KOREN"/stend/*.py; do
  [ -e "$put" ] || continue
  imya=$(basename "$put")
  if est_v "$imya" "${NE_STEND[@]}"; then
    PROPUSK+=("$imya — подсобка, не стенд")
    continue
  fi
  if est_v "$imya" "${NUZHEN_WINE[@]}" && [ "${BEZ_WINE:-0}" = "1" ]; then
    PROPUSK+=("$imya — нужен wine, а BEZ_WINE=1")
    continue
  fi
  case "$imya" in
    *.py) shag "$imya" python3 "$put" ;;
    *)    shag "$imya" bash "$put" ;;
  esac
done

printf '\n════════ ИТОГ ПРИЁМКИ ════════\n'
for i in "${ZELENYE[@]}"; do printf '  🟢 %s\n' "$i"; done
for i in "${PROPUSK[@]}"; do printf '  ⚪ пропущен: %s\n' "$i"; done
for i in "${KRASNYE[@]}"; do printf '  🔴 %s\n' "$i"; done
printf 'зелёных %d, пропущено %d, красных %d\n' \
  "${#ZELENYE[@]}" "${#PROPUSK[@]}" "${#KRASNYE[@]}"
[ "${#KRASNYE[@]}" -eq 0 ] || exit 1
