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
NE_STEND=("vse.sh" "znachok.py" "obshchee.sh")
# Нужен wine: под ним гоняются настоящие windows-бинарники.
NUZHEN_WINE=("windows.sh" "razdvoenie.sh" "trey.sh" "obnovlenie.sh" "proksi.sh")

est_v() { local i n=$1; shift; for i in "$@"; do [ "$i" = "$n" ] && return 0; done; return 1; }

ZELENYE=(); KRASNYE=(); PROPUSK=(); MERTVYE=()

shag() {  # shag <имя> <команда...>
  local imya=$1; shift
  printf '\n═══ %s\n' "$imya"
  if timeout "$TAYMAUT" "$@"; then
    ZELENYE+=("$imya")
  else
    local rc=$?
    printf '   ✗ %s: rc=%d\n' "$imya" "$rc"
    # rc=7 — сигнал стенда «прибор мёртв» (wine упал раньше, чем продукт вообще
    # начал проверяться, см. stend/obshchee.sh): это не брак продукта, класть
    # в отдельную группу, а не путать с настоящим красным.
    if [ "$rc" -eq 7 ]; then
      MERTVYE+=("$imya")
    else
      KRASNYE+=("$imya (rc=$rc)")
    fi
  fi
}

cd "$KOREN" || exit 1
shag "go build" go build ./...
shag "go test" go test ./...

# TestYadroPrinimaetGotovyyKonfig щупает конфиг настоящим ядром (sing-box
# check) и МОЛЧА пропускается, если бинаря нет, — `go test ./...` выше
# зеленеет что с ядром, что без него, а «зелень» без ядра означает лишь
# «конфиг согласен сам с собой» (та беда, что уже дважды роняла старт у
# человека: override_android_vpn, store_selected). Здесь, в приёмке, тишина
# недопустима: нет ядра — красный с прямой строкой, а не молчаливый пропуск.
shag "ядро настоящее (не пропущен щуп)" bash -c '
  [ -n "${KELEVRA_YADRO:-}" ] && [ -f "$KELEVRA_YADRO" ] && exit 0
  for p in "'"$KOREN"'/.stend/dom/yadro/sing-box" "'"$KOREN"'/.stend/sing-box-linux"; do
    [ -f "$p" ] && exit 0
  done
  echo "настоящего ядра нет ни в KELEVRA_YADRO, ни в $KOREN/.stend/{dom/yadro/sing-box,sing-box-linux} — щуп TestYadroPrinimaetGotovyyKonfig пропущен, приёмка это доказательство НЕ считает" >&2
  exit 1
'

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
for i in "${MERTVYE[@]}"; do printf '  ⚫ %s — прибор мёртв, не проверено\n' "$i"; done
for i in "${KRASNYE[@]}"; do printf '  🔴 %s\n' "$i"; done
printf 'зелёных %d, пропущено %d, мёртвых %d, красных %d\n' \
  "${#ZELENYE[@]}" "${#PROPUSK[@]}" "${#MERTVYE[@]}" "${#KRASNYE[@]}"
{ [ "${#KRASNYE[@]}" -eq 0 ] && [ "${#MERTVYE[@]}" -eq 0 ]; } || exit 1
