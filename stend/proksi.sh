#!/usr/bin/env bash
# Стенд «снятие системного прокси»: доказывает живьём (настоящая windows-сборка
# под wine), что кнопка «Отключить» и авария приложения выключают ProxyEnable
# в реестре Windows, а не только выход из окна.
#
# Зачем отдельно от windows.sh. Диагноз 20.08: ядро прописывает себя системным
# прокси само, а откатывает запись только при вежливом выходе — которого на
# Windows нет. До сегодняшней правки proksi.Snyat() звался ТОЛЬКО в конце main
# после закрытия окна: нажатие «Отключить» (ручка /api/otklyuchit) оставляло
# приложение открытым и прокси включённым — сайты переставали грузиться у
# человека, который ничего не закрывал. Этот стенд гоняет настоящий
# Kelevra.exe под wine и проверяет реестр ДО и ПОСЛЕ вызова ручки — линуксовый
# двойник этого не покажет, там до реестра Windows кода не доходит.
#
# Обвязка wine — та же, что в windows.sh (см. его шапку про кириллицу и Xvfb).
set -u
KORFN=$(cd "$(dirname "$0")/.." && pwd)
WINE=${WINE:-/usr/lib/wine/wine64}
export WINEPREFIX=${WINEPREFIX:-$KORFN/.wine}
export WINEDEBUG=${WINEDEBUG:--all}
export HOME=${HOME:-/root}
export LANG=${LANG:-C.UTF-8} LC_ALL=${LC_ALL:-C.UTF-8}
# wine падает («double free or corruption (out)»), если TMPDIR унаследован с
# диска (замерено 23.08, воспроизведено с TMPDIR на ZFS-каталоге): ему нужен
# настоящий /tmp. GOTMPDIR/GOCACHE у go приоритетнее TMPDIR, сборке не мешает.
export TMPDIR=/tmp
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

NASTR='HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings'
ZHURNAL="$WINEPREFIX/drive_c/users/$(whoami)/AppData/Local/Kelevra/kelevra.log"
bed=0

reg_set() { # $1 ProxyEnable(0|1)  $2 ProxyServer
  "$WINE" reg add "$NASTR" /v ProxyEnable /t REG_DWORD /d "$1" /f >/dev/null 2>&1
  "$WINE" reg add "$NASTR" /v ProxyServer /t REG_SZ /d "$2" /f >/dev/null 2>&1
}
reg_get() { # $1 имя значения -> печатает значение (или пусто)
  # wine пишет вывод с \r\n (виндовый формат): без tr -d '\r' сравнение строк
  # в скрипте молча ломается — 0x0 с хвостовым CR не равен чистому "0x0".
  "$WINE" reg query "$NASTR" /v "$1" 2>/dev/null | tr -d '\r' | awk -v v="$1" '$1==v {print $3}'
}

# запускает Kelevra.exe в служебном режиме, возвращает URL службы через echo.
# Возврат функции 77 (через $?) значит «прибор мёртв» — см. stend/obshchee.sh.
zapustit_i_vzyat_url() { # $1 = файл для журнала запуска
  # KELEVRA_BEZ_OBNOVLENIYA=1 — иначе версия по умолчанию (0.1.0-rabota) видит
  # на GitHub релиз новее, тихо подменяет себя настоящим релизом и
  # перезапускается ИМ: стенд честно проверял бы чужой бинарь без нашей правки
  # (поймано 20.08: журнал показал "обновление: вышла версия 0.5.1, ставлю").
  wine_zapusti "$1" "$ZHURNAL" "служба слушает" 20 -- \
    env KELEVRA_BEZ_OKNA=1 KELEVRA_BEZ_OBNOVLENIYA=1 timeout 30 "$WINE" "$STEND/Kelevra.exe"
  local rc=$?
  grep -o 'http://[^ ]*' "$ZHURNAL" 2>/dev/null | tail -1
  return "$rc"
}

ostanovit() { # гасит процесс стенда по короткому имени: /proc/PID/cmdline у
  # wine-процесса — виндовый путь "Z:\...\Kelevra.exe" (обратные слэши, без
  # $STEND), полный POSIX-путь в нём никогда не совпадёт. SIGTERM не всегда
  # доходит до эмулированного Windows-процесса, поэтому ждём и добиваем SIGKILL.
  pkill -TERM -f Kelevra.exe 2>/dev/null
  for _ in $(seq 1 10); do
    pgrep -f Kelevra.exe >/dev/null 2>&1 || return 0
    sleep 1
  done
  pkill -KILL -f Kelevra.exe 2>/dev/null
  sleep 1
}

echo "── сборка Kelevra.exe (windows/amd64) ──"
if ! GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui" -o "$STEND/Kelevra.exe" "$KORFN/cmd/kelevra" 2>&1; then
  echo "  НЕ СОБРАЛСЯ"; exit 1
fi

echo "── сценарий 1: прокси был включён нами, «Отключить» должен снять его ──"
reg_set 1 127.0.0.1:2412
do_before=$(reg_get ProxyEnable); server_before=$(reg_get ProxyServer)
echo "  до: ProxyEnable=$do_before ProxyServer=$server_before"
url=$(zapustit_i_vzyat_url "$STEND/proksi_zapusk1.log")
if [ "$?" -eq 77 ]; then
  echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
  exit 7
fi
if [ -z "$url" ]; then
  echo "  служба не поднялась, журнал:"; tail -8 "$ZHURNAL" 2>/dev/null; bed=1
else
  echo "  служба: $url"
  otvet=$(curl -s -o /dev/null -w '%{http_code}' "${url}api/otklyuchit")
  echo "  GET ${url}api/otklyuchit -> код $otvet"
  sleep 1
  posle=$(reg_get ProxyEnable); server_posle=$(reg_get ProxyServer)
  echo "  после: ProxyEnable=$posle ProxyServer=$server_posle"
  if [ "$posle" != "0x0" ]; then
    echo "  КРАСНЫЙ: ProxyEnable не снят (осталось $posle)"; bed=1
  elif [ "$server_posle" != "$server_before" ]; then
    echo "  КРАСНЫЙ: ProxyServer тронут (было $server_before, стало $server_posle)"; bed=1
  else
    echo "  зелёный: прокси снят, адрес не тронут"
  fi
fi
ostanovit

echo "── сценарий 2: прокси и так выключен, «Отключить» не должен трогать чужую настройку ──"
reg_set 0 10.0.0.9:9999
do_before=$(reg_get ProxyEnable); server_before=$(reg_get ProxyServer)
echo "  до: ProxyEnable=$do_before ProxyServer=$server_before"
url=$(zapustit_i_vzyat_url "$STEND/proksi_zapusk2.log")
if [ "$?" -eq 77 ]; then
  echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
  exit 7
fi
if [ -z "$url" ]; then
  echo "  служба не поднялась, журнал:"; tail -8 "$ZHURNAL" 2>/dev/null; bed=1
else
  echo "  служба: $url"
  otvet=$(curl -s -o /dev/null -w '%{http_code}' "${url}api/otklyuchit")
  echo "  GET ${url}api/otklyuchit -> код $otvet"
  sleep 1
  posle=$(reg_get ProxyEnable); server_posle=$(reg_get ProxyServer)
  echo "  после: ProxyEnable=$posle ProxyServer=$server_posle"
  if [ "$posle" != "0x0" ]; then
    echo "  КРАСНЫЙ: ProxyEnable сам включился ($posle)"; bed=1
  elif [ "$server_posle" != "$server_before" ]; then
    echo "  КРАСНЫЙ: чужой ProxyServer тронут без включённого прокси (было $server_before, стало $server_posle)"; bed=1
  else
    echo "  зелёный: чужая выключенная настройка не тронута"
  fi
fi
ostanovit

echo "── сценарий 3: ядро прописало прокси и упало при старте — «Подключить» должен снять его тоже ──"
# Диагноз 20.08 (второй заход): ядро прописывает системный прокси ДО того, как
# ответит его Clash API — Zapustit() может упасть или не дождаться API за
# 70 секунд уже ПОСЛЕ этого. До сегодняшней правки proksi.Snyat() в ручке
# /api/podklyuchit не звался вовсе: неудачное подключение оставляло прокси
# висеть, а «Отключить» человек в этот момент ещё не нажимал.
PROFIL="$WINEPREFIX/drive_c/users/$(whoami)/AppData/Local/Kelevra/profil.json"
YADRO_PAPKA="$WINEPREFIX/drive_c/users/$(whoami)/AppData/Local/Kelevra/yadro"
mkdir -p "$YADRO_PAPKA"
cp "$KORFN/internal/konfig/testdata/profil_telefona.json" "$PROFIL"
printf 'не ядро, а мусор — Windows не сможет это запустить' > "$YADRO_PAPKA/sing-box.exe"
reg_set 1 127.0.0.1:2412
do_before=$(reg_get ProxyEnable); server_before=$(reg_get ProxyServer)
echo "  до: ProxyEnable=$do_before ProxyServer=$server_before (профиль и битое ядро подложены руками)"
url=$(zapustit_i_vzyat_url "$STEND/proksi_zapusk3.log")
if [ "$?" -eq 77 ]; then
  echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
  exit 7
fi
if [ -z "$url" ]; then
  echo "  служба не поднялась, журнал:"; tail -8 "$ZHURNAL" 2>/dev/null; bed=1
else
  echo "  служба: $url"
  otvet=$(curl -s -o /dev/null -w '%{http_code}' "${url}api/podklyuchit")
  echo "  GET ${url}api/podklyuchit -> код $otvet (ждём отказ: ядро битое)"
  sleep 1
  posle=$(reg_get ProxyEnable); server_posle=$(reg_get ProxyServer)
  echo "  после: ProxyEnable=$posle ProxyServer=$server_posle"
  if [ "$posle" != "0x0" ]; then
    echo "  КРАСНЫЙ: ProxyEnable не снят после неудачного подключения (осталось $posle)"; bed=1
  else
    echo "  зелёный: неудачное подключение тоже сняло прокси"
  fi
fi
ostanovit

echo "── итог: $([ $bed -eq 0 ] && echo ЗЕЛЁНЫЙ || echo КРАСНЫЙ) ──"
exit $bed
