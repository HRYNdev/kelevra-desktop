#!/usr/bin/env bash
# Стенд «гонка двух запусков»: доказывает живьём (настоящая windows-сборка
# под wine), что БЫСТРЫЙ повторный запуск Kelevra.exe может поднять ДВЕ
# независимые пары окно+служба вместо одной.
#
# Жалоба хозяина 23.08: три фото Диспетчера задач с несколькими Kelevra.exe
# одновременно («(3)», «(9)») и ошибка при отключении, когда открыты
# 2 приложения.
#
# Диагноз (internal/kopiya/kopiya.go, cmd/kelevra/main.go): Nayti() и
# zapustitOtdelnuyuSluzhbu() не атомарны между собой. main() читает метку
# (Nayti), не находит — и поднимает свою службу отдельным процессом; если
# второй .exe стартует ДО того, как первая служба успела подняться и
# записать метку, второй тоже не находит первую копию и тоже поднимает
# свою службу. Этот стенд запускает --tiho ДВАЖДЫ подряд без паузы и
# считает, сколько unix-процессов "Kelevra.exe --sluzhba" выжило.
#
# Почему --tiho: тот же путь до окна включительно (см. razdvoenie.sh),
# без WebView2, которого под wine нет.
set -u
KORFN=$(cd "$(dirname "$0")/.." && pwd)
WINE=${WINE:-/usr/lib/wine/wine64}
export WINEPREFIX=${WINEPREFIX:-$KORFN/.wine}
export WINEDEBUG=${WINEDEBUG:--all}
export HOME=${HOME:-/root}
export LANG=${LANG:-C.UTF-8} LC_ALL=${LC_ALL:-C.UTF-8}
STEND=$KORFN/.stend_win
mkdir -p "$STEND" "$WINEPREFIX"
. "$KORFN/stend/obshchee.sh"

command -v go >/dev/null 2>&1 || export PATH="$PATH:/usr/local/go/bin"

if [ ! -x "$WINE" ]; then
  echo "нет wine ($WINE): apt-get install -y --no-install-recommends wine64" >&2
  exit 2
fi

if ! xdpyinfo -display :97 >/dev/null 2>&1; then
  Xvfb :97 -screen 0 1280x800x24 >/dev/null 2>&1 &
  sleep 2
fi
export DISPLAY=${DISPLAY:-:97}

# Сколько раз повторить гонку: разовый прогон не аргумент (гонки нестабильны).
POVTOROV=${POVTOROV:-5}

PAPKA="$WINEPREFIX/drive_c/users/$(whoami)/AppData/Local/Kelevra"
ZHURNAL="$PAPKA/kelevra.log"
METKA="$PAPKA/zapushcheno.json"

echo "── сборка Kelevra.exe (windows/amd64) ──"
if ! GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui" -o "$STEND/Kelevra.exe" "$KORFN/cmd/kelevra" 2>&1; then
  echo "  НЕ СОБРАЛСЯ"; exit 1
fi

# Одиночный прогон --tiho первым — самый дешёвый способ поймать «прибор
# мёртв» раньше, чем тратить время на гонку целиком (см. obshchee.sh).
pkill -f "Kelevra.exe" 2>/dev/null; sleep 1
rm -f "$ZHURNAL" "$METKA"
wine_zapusti "$STEND/probnik.log" "$ZHURNAL" - 20 -- \
  env KELEVRA_BEZ_OBNOVLENIYA=1 timeout 20 "$WINE" "$STEND/Kelevra.exe" --tiho
probnik=$?
pkill -f "Kelevra.exe" 2>/dev/null; sleep 1
if [ "$probnik" -eq 77 ]; then
  echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
  exit 7
fi

gonok=0
odinoznak=0
for i in $(seq 1 "$POVTOROV"); do
  echo "── гонка №$i ──"
  pkill -f "Kelevra.exe" 2>/dev/null; sleep 1
  rm -f "$ZHURNAL" "$METKA" "$STEND/gonka_a_$i.log" "$STEND/gonka_b_$i.log"

  # Два запуска БЕЗ паузы между ними — сердце гонки: обе копии не должны
  # успеть увидеть метку друг друга.
  env KELEVRA_BEZ_OBNOVLENIYA=1 timeout 25 "$WINE" "$STEND/Kelevra.exe" --tiho \
    >"$STEND/gonka_a_$i.log" 2>&1 &
  PID_A=$!
  env KELEVRA_BEZ_OBNOVLENIYA=1 timeout 25 "$WINE" "$STEND/Kelevra.exe" --tiho \
    >"$STEND/gonka_b_$i.log" 2>&1 &
  PID_B=$!

  # Ждём оба родителя (--tiho обязан выйти сам — см. razdvoenie.sh); лимит —
  # подстраховка на случай, если оба зависнут (не должны).
  predel=$((SECONDS + 30))
  while kill -0 "$PID_A" 2>/dev/null || kill -0 "$PID_B" 2>/dev/null; do
    [ "$SECONDS" -ge "$predel" ] && break
    sleep 0.3
  done
  wait "$PID_A" 2>/dev/null; wait "$PID_B" 2>/dev/null

  # Даём службам долю секунды дописать метку/лог после того, как родители вышли.
  sleep 1

  sluzhb=$(pgrep -f "Kelevra.exe --sluzhba" | wc -l)
  echo "  unix-процессов службы (Kelevra.exe --sluzhba): $sluzhb"
  if [ -f "$METKA" ]; then
    adres=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))['url'])" "$METKA" 2>/dev/null)
    echo "  метка сейчас указывает на: $adres"
  fi

  if [ "$sluzhb" -gt 1 ]; then
    echo "  КРАСНЫЙ: гонка сработала — $sluzhb независимых служб вместо одной"
    gonok=$((gonok + 1))
  elif [ "$sluzhb" -eq 1 ]; then
    echo "  ровно одна служба"
    odinoznak=$((odinoznak + 1))
  else
    echo "  0 служб — ни одна не поднялась (не гонка, отдельная беда)"
  fi

  pkill -f "Kelevra.exe" 2>/dev/null
done

echo "── журнал последней гонки (a) ──"
tail -12 "$STEND/gonka_a_$POVTOROV.log" 2>/dev/null | sed 's/^/  a: /'
echo "── журнал последней гонки (b) ──"
tail -12 "$STEND/gonka_b_$POVTOROV.log" 2>/dev/null | sed 's/^/  b: /'
echo "── гонки: $gonok из $POVTOROV прогонов дали больше одной службы, $odinoznak дали ровно одну ──"

# ─────────────────────────────────────────────────────────────────────
# Сценарий 2: ШТАТНЫЙ повторный запуск при живой копии — не сломал ли замок
# то, что и так работало. Второй .exe обязан найти метку, показать чужую
# копию и не поднять второго ядра.
#
# ЧЕГО ЭТОТ СЦЕНАРИЙ НЕ СУДИТ, честно: он НЕ ловит замок, задержавшийся до
# конца процесса. Под --tiho первый .exe выходит сразу после подъёма службы
# (окна нет — нет и WebView2 под wine), так что к моменту второго запуска
# он отпустит любой замок, хоть узкий, хоть до конца процесса. Проверено
# живьём: на замке до конца процесса этот сценарий остался зелёным. Отсюда
# и решение держать замок формой, а не стендом (см. adresKopii в main.go):
# окно показывается снаружи функции с замком, и продлить его нечем.
PREDEL_VTOROGO=${PREDEL_VTOROGO:-15}
echo "── сценарий 2: повторный запуск при живой копии (предел $PREDEL_VTOROGO с) ──"
pkill -f "Kelevra.exe" 2>/dev/null; sleep 1
rm -f "$ZHURNAL" "$METKA"
env KELEVRA_BEZ_OBNOVLENIYA=1 timeout 40 "$WINE" "$STEND/Kelevra.exe" --tiho \
  >"$STEND/shtatnyy_a.log" 2>&1 &
PID_A=$!
# Ждём именно МЕТКУ, а не время: копия считается работающей ровно с того
# момента, как её видно следующему запуску.
predel=$((SECONDS + 30))
while [ ! -f "$METKA" ] && [ "$SECONDS" -lt "$predel" ]; do sleep 0.2; done
wait "$PID_A" 2>/dev/null
if [ ! -f "$METKA" ]; then
  echo "  ⚫ ПРИБОР МЁРТВ: первая копия не отметилась — сценарий 2 ничего не измерил"
  vtoroy_rc=7
else
  nachalo=$SECONDS
  env KELEVRA_BEZ_OBNOVLENIYA=1 timeout 40 "$WINE" "$STEND/Kelevra.exe" --tiho \
    >"$STEND/shtatnyy_b.log" 2>&1
  proshlo=$((SECONDS - nachalo))
  echo "  второй запуск отработал за ${proshlo} с"
  grep -q "копия уже запущена" "$STEND/shtatnyy_b.log" \
    && echo "  второй запуск увидел копию (не поднимал своё ядро)" \
    || echo "  ⚠ в журнале второго запуска НЕТ «копия уже запущена»"
  if [ "$proshlo" -gt "$PREDEL_VTOROGO" ]; then
    echo "  КРАСНЫЙ: штатный повторный запуск ждал ${proshlo} с — человек щёлкнул по значку и ничего не увидел"
    vtoroy_rc=1
  else
    echo "  ровно то, что нужно: копия показана без ожидания"
    vtoroy_rc=0
  fi
fi
pkill -f "Kelevra.exe" 2>/dev/null

# ─────────────────────────────────────────────────────────────────────
# Сценарий 3: второй процесс БЕЗ --tiho при живой копии не должен слепо
# создавать своё окно поверх чужого (беда 23.08: два независимых окна,
# второе шлёт podklyuchit на уже работающее ядро — см. adresKopii/
# podnyatChuzheeOkno в main.go и okno_windows.go).
#
# ЧЕСТНО, чего этот сценарий НЕ СУДИТ: под wine компонента WebView2 нет
# вовсе (замерено в windows.sh), поэтому ни одна копия не открывает
# настоящее окно — положительную ветку «нашёл чужое окно и поднял его»
# здесь показать нечем. Сценарий проверяет только то, что доказуемо
# живьём: второй процесс ПЫТАЕТСЯ найти чужое окно (зовёт FindWindowW по
# классу/заголовку) вместо того, чтобы сразу пойти в pokazatOkno, как было
# до правки.
echo "── сценарий 3: без --tiho — второй процесс обязан ПОПРОБОВАТЬ поднять чужое окно ──"
pkill -f "Kelevra.exe" 2>/dev/null; sleep 1
rm -f "$ZHURNAL" "$METKA"
env KELEVRA_BEZ_OBNOVLENIYA=1 timeout 40 "$WINE" "$STEND/Kelevra.exe" \
  >"$STEND/okno_a.log" 2>&1 &
PID_A=$!
predel=$((SECONDS + 30))
while [ ! -f "$METKA" ] && [ "$SECONDS" -lt "$predel" ]; do sleep 0.2; done
wait "$PID_A" 2>/dev/null

if [ ! -f "$METKA" ]; then
  echo "  ⚫ ПРИБОР МЁРТВ: первая копия не отметилась — сценарий 3 ничего не измерил"
  tretiy_rc=7
else
  env KELEVRA_BEZ_OBNOVLENIYA=1 timeout 40 "$WINE" "$STEND/Kelevra.exe" \
    >"$STEND/okno_b.log" 2>&1
  echo "── журнал второго процесса (без --tiho) ──"
  tail -20 "$STEND/okno_b.log" | sed 's/^/  b: /'
  if grep -q "пробую поднять её окно вместо создания своего" "$STEND/okno_b.log"; then
    echo "  зелёный: второй процесс попробовал поднять чужое окно (не создал своё вслепую)"
    tretiy_rc=0
  else
    echo "  КРАСНЫЙ: в журнале второго процесса нет попытки поднять чужое окно"
    tretiy_rc=1
  fi
  echo "  ЧЕСТНО: под wine WebView2 нет — «нашёл и поднял чужое окно» этим сценарием не доказано,"
  echo "  доказан только сам факт попытки вместо слепого создания своего."
fi
pkill -f "Kelevra.exe" 2>/dev/null

echo "── итог ──"
[ "$gonok" -gt 0 ] && echo "КРАСНЫЙ 1/3: гонка воспроизведена ($gonok/$POVTOROV)"
[ "$gonok" -eq 0 ] && echo "зелёный 1/3: ни разу больше одной службы ($odinoznak/$POVTOROV)"
[ "$vtoroy_rc" -eq 0 ] && echo "зелёный 2/3: штатный повторный запуск не ждёт замка"
[ "$tretiy_rc" -eq 0 ] && echo "зелёный 3/3: второй процесс без --tiho пробует поднять чужое окно"
[ "$gonok" -gt 0 ] && exit 1
[ "$vtoroy_rc" -ne 0 ] && exit "$vtoroy_rc"
[ "$tretiy_rc" -ne 0 ] && exit "$tretiy_rc"
echo "ЗЕЛЁНЫЙ: все три сценария"
exit 0
