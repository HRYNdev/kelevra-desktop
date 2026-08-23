#!/usr/bin/env bash
# Стенд Windows: гоняет НАСТОЯЩУЮ сборку под Windows, а не её линуксовый двойник.
#
# Зачем. Продукт живёт только на Windows, а собирается и проверяется здесь. Пока
# проверка шла линуксовой сборкой, зелёные тесты означали лишь «код согласен сам
# с собой»: пути %LOCALAPPDATA%, запуск sing-box.exe, скрытие консоли, вторая
# копия по файлу-метке — всё это на Linux просто другой код. Стенд запускает
# windows/amd64 бинарники через wine, поэтому исполняется тот же код, что у
# человека.
#
# Что стенд ПОКАЗЫВАЕТ:
#   · тесты пакетов konfig/yadro/kopiya/podpiska на windows-сборке;
#   · старт самого Kelevra.exe (служебный режим KELEVRA_BEZ_OKNA=1);
#   · старт настоящего sing-box.exe для Windows с нашим готовым профилем.
# Чего стенд НЕ ЗАМЕНЯЕТ (замерено 20.08):
#   · окно: WebView2 под wine не поднимается, там нужна живая Windows;
#   · боевой трафик: wine не отдаёт ядру список сетевых адаптеров
#     («getadaptersaddresses: Invalid data»), и ядро считает, что интернета нет.
# То есть зелёный стенд = «до окна и до сети всё честно доехало», не более.
#
# Подготовка (один раз): apt-get install -y --no-install-recommends wine64 xvfb
set -u
KORFN=$(cd "$(dirname "$0")/.." && pwd)
WINE=${WINE:-/usr/lib/wine/wine64}
export WINEPREFIX=${WINEPREFIX:-$KORFN/.wine}
export WINEDEBUG=${WINEDEBUG:--all}
export HOME=${HOME:-/root}
# Кириллица в путях: без UTF-8 локали wine не создаёт такие папки вовсе
# (замерено 20.08: mkdir «кириллица_проба» → File not found), и тесты с русскими
# именами подтестов краснеют на ровном месте. Это про стенд, а не про продукт.
export LANG=${LANG:-C.UTF-8} LC_ALL=${LC_ALL:-C.UTF-8}
# wine падает («double free or corruption (out)»), если TMPDIR унаследован с
# диска (замерено 23.08, воспроизведено с TMPDIR на ZFS-каталоге): ему нужен
# настоящий /tmp. GOTMPDIR/GOCACHE у go приоритетнее TMPDIR, сборке не мешает.
export TMPDIR=/tmp
# Без go стенд не стенд, а имитация: собрать нечего, и весь смысл теряется.
# Говорим это ОДИН раз и громко, а не десять раз «command not found» внутри цикла.
command -v go >/dev/null 2>&1 || { echo "СТЕНД НЕ ЗАПУЩЕН: go нет в PATH (обычно /usr/local/go/bin; зови через bash -lc)"; exit 2; }
STEND=$KORFN/.stend_win
mkdir -p "$STEND" "$WINEPREFIX"
. "$KORFN/stend/obshchee.sh"

if [ ! -x "$WINE" ]; then
  echo "нет wine ($WINE): apt-get install -y --no-install-recommends wine64" >&2
  exit 2
fi

# Экран нужен даже без окна: без него wine ругается на каждый запуск.
if ! xdpyinfo -display :97 >/dev/null 2>&1; then
  Xvfb :97 -screen 0 1280x800x24 >/dev/null 2>&1 &
  sleep 2
fi
export DISPLAY=${DISPLAY:-:97}

bed=0
echo "── ядро для Windows ──"
YADRO_URL=${YADRO_URL:-https://github.com/HRYNdev/kelevra-desktop/releases/download/core-v1.14.0-beta.4-1/sing-box-windows-amd64.zip}
if [ ! -f "$STEND/sing-box.exe" ]; then
  echo "  качаю ядро для Windows из релиза"
  curl -sL -o "$STEND/core.zip" "$YADRO_URL" && (cd "$STEND" && python3 -c "import zipfile;zipfile.ZipFile('core.zip').extractall('.')")
fi
if [ ! -f "$STEND/sing-box.exe" ]; then
  echo "  ядро не добыть: $YADRO_URL"; bed=1
else
  # Первый настоящий вызов wine в этом стенде — дешёвый и самый ранний, им же
  # ловим смерть прибора (см. stend/obshchee.sh), пока не потрачено время на
  # тесты пакетов и старт Kelevra.exe ниже.
  wine_zapusti "$STEND/sing_version.log" - - 60 -- timeout 60 "$WINE" "$STEND/sing-box.exe" version
  if [ "$?" -eq 77 ]; then
    echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
    exit 7
  fi
  v=$(head -1 "$STEND/sing_version.log")
  echo "  $v"
  printf '%s' "$v" | grep -q "sing-box version" || bed=1
fi


# Щуп «настоящее ядро принимает наш конфиг» должен звать ЯДРО ДЛЯ WINDOWS:
# иначе он молча пропускается или зовёт линуксовый бинарь, которого под wine нет.
export KELEVRA_YADRO="Z:$(printf '%s' "$STEND" | tr '/' '\\')\\sing-box.exe"

echo "── тесты windows-сборкой ──"
# Список пакетов НЕ перечисляем руками: 20.08 пакет avtozapusk выпал из приёмки
# молча — на Linux он не собирается (//go:build windows), а в захардкоженном
# списке его забыли, и тест реестра не исполнился ни разу нигде. Берём всё, что
# лежит в internal/, чтобы следующий новый пакет не пропал тем же способом.
for p in $(cd "$KORFN/internal" && ls -d */ 2>/dev/null | tr -d '/'); do
  [ -d "$KORFN/internal/$p" ] || continue
  # Старый бинарь сносим ДО сборки. Иначе несобравшийся пакет исполняет
  # позавчерашний .exe и печатает «зелёный» про код, которого уже нет
  # (22.08: без go в PATH стенд так отчитался за 6 пакетов из 10).
  rm -f "$STEND/t_$p.exe"
  sborka=$(GOOS=windows GOARCH=amd64 go test -c -o "$STEND/t_$p.exe" "./internal/$p" 2>&1)
  if printf '%s' "$sborka" | grep -q "no test files"; then echo "  $p: тестов нет"; continue; fi
  [ -f "$STEND/t_$p.exe" ] || { echo "  $p: НЕ СОБРАЛСЯ"; printf '%s\n' "$sborka" | tail -3; bed=1; continue; }
  # cwd = папка пакета: тесты читают testdata по относительному пути.
  vyvod=$(cd "$KORFN/internal/$p" && timeout 400 "$WINE" "$STEND/t_$p.exe" 2>&1)
  if printf '%s' "$vyvod" | grep -q "^FAIL"; then
    echo "  $p: КРАСНЫЙ"; printf '%s\n' "$vyvod" | grep -E "^ *---|\.go:" | head -8; bed=1
  else
    echo "  $p: зелёный"
  fi
done

echo "── старт Kelevra.exe (служебный режим) ──"
GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui" -o "$STEND/Kelevra.exe" "$KORFN/cmd/kelevra" || bed=1
zhurnal="$WINEPREFIX/drive_c/users/$(whoami)/AppData/Local/Kelevra/kelevra.log"
wine_zapusti "$STEND/zapusk.log" "$zhurnal" "служба слушает" 12 -- \
  env KELEVRA_BEZ_OKNA=1 timeout 25 "$WINE" "$STEND/Kelevra.exe"
if [ "$?" -eq 77 ]; then
  echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
  exit 7
fi
if grep -q "служба слушает" "$zhurnal" 2>/dev/null; then
  echo "  служба поднялась: $(grep -o 'http://[^ ]*' "$zhurnal" | tail -1)"
else
  echo "  служба НЕ поднялась, журнал:"; tail -5 "$zhurnal" 2>/dev/null; bed=1
fi
pkill -f "$STEND/Kelevra.exe" 2>/dev/null

echo "── итог: $([ $bed -eq 0 ] && echo ЗЕЛЁНЫЙ || echo КРАСНЫЙ) ──"
exit $bed
