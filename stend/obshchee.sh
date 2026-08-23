#!/usr/bin/env bash
# Общая обвязка wine-стендов: одна функция запуска exe под wine, которая
# отличает СМЕРТЬ ПРИБОРА (сам wine упал/ничего не сделал) от обычного
# провала продукта.
#
# Диагноз 23.08: на этой машине wine стал падать одной строкой в stderr —
# `free(): invalid pointer` — на любом PE-файле, ещё до того, как приложение
# успевает написать хоть одну строку в свой лог. Каждый стенд читал в этом
# случае пустой/чужой лог и честно писал «КРАСНЫЙ», хотя продукт вообще не
# проверялся: судили прибор, а не вещь. Эта обёртка ловит именно такую смерть
# и даёт стенду отдельный код возврата (77 у функции, exit 7 у стенда) —
# чтобы приёмка (stend/vse.sh) могла отличить «продукт сломан» от «щуп сегодня
# не может смотреть».
#
# Использование:
#   wine_zapusti <log_zapuska> <zhurnal_prilozheniya|-> <stroka_ozhidaniya|-> <taimaut_sek> [--] <komanda...>
#     log_zapuska         — куда пишется stdout+stderr самого запуска (сюда же
#                            падает «free(): invalid pointer», если wine мёртв)
#     zhurnal_prilozheniya— файл, в который приложение само пишет свой лог,
#                            или "-", если такого файла нет (тогда смотрим log_zapuska)
#     stroka_ozhidaniya   — grep-шаблон строки, которую ждём в журнале, чтобы
#                            выйти из цикла раньше таймаута, или "-", если её нет
#     taimaut_sek         — сколько секунд ждать (целое число, посекундный опрос)
#     komanda...          — сама команда запуска (обычно "$WINE" exe ...)
#   Возврат: 0 — прибор жив, дальше стенд судит продукт как раньше;
#            77 — прибор мёртв, стенд обязан напечатать
#                 «⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в
#                 логе) — продукт НЕ проверялся» и exit 7.
#   Побочно выставляет WINE_ZAPUSTI_PID — unix-pid фонового процесса запуска,
#   если он нужен вызывающему стенду дальше (kill/wait/pgrep).
wine_zapusti() {
  local log=$1 zhurnal=$2 stroka=$3 taimaut=$4
  shift 4
  [ "${1:-}" = "--" ] && shift

  rm -f "$log"
  [ "$zhurnal" = "-" ] || rm -f "$zhurnal"

  "$@" >"$log" 2>&1 &
  WINE_ZAPUSTI_PID=$!
  local pid=$WINE_ZAPUSTI_PID i=0
  while [ "$i" -lt "$taimaut" ]; do
    if [ "$zhurnal" != "-" ] && [ "$stroka" != "-" ] && grep -q "$stroka" "$zhurnal" 2>/dev/null; then
      break
    fi
    kill -0 "$pid" 2>/dev/null || break
    sleep 1
    i=$((i + 1))
  done

  # цель проверки «пусто ли»: файл, в котором вообще должна была появиться
  # хоть одна строка — журнал приложения, если он есть, иначе сам log_zapuska.
  local cel=$zhurnal
  [ "$cel" = "-" ] && cel=$log

  if grep -qE 'free\(\): invalid pointer|double free|corruption' "$log" 2>/dev/null; then
    return 77
  fi
  if [ ! -s "$cel" ] && { [ "$stroka" = "-" ] || ! grep -q "$stroka" "$cel" 2>/dev/null; }; then
    return 77
  fi
  return 0
}
