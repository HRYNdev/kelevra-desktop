#!/usr/bin/env bash
# Стенд «два процесса вместо одного»: доказывает живьём (настоящая windows-
# сборка под wine), что режим --tiho поднимает службу ОТДЕЛЬНЫМ, отсоединённым
# процессом и не утаскивает её за собой, когда сам выходит.
#
# Диагноз 20.08: окно и служба (ядро sing-box + системный прокси + HTTP-служба)
# жили в ОДНОМ процессе cmd/kelevra/main.go. Крестик на окне гасил процесс
# целиком — ядро останавливалось, прокси не снимался (реестр остаётся
# висеть), а человек считал, что просто закрыл окно. Лекарство — развести окно
# и службу на два процесса одного .exe; этот стенд проверяет ровно
# новую часть: что после выхода процесса, который поднимал службу, служба
# (и, значит, защита) продолжает жить сама по себе.
#
# Зачем отдельно от windows.sh. windows.sh гоняет службу НАПРЯМУЮ
# (KELEVRA_BEZ_OKNA=1, синоним --sluzhba) — там всегда был один процесс,
# и это не показывает сам факт разведения на два. Этот стенд запускает
# ВЕРХНИЙ уровень (--tiho) и смотрит на ДВА процесса: тот, что запустили мы,
# и тот, что запустил он сам.
#
# Почему --tiho, а не совсем без аргументов. Без аргументов (двойной щелчок)
# в конце открывается окно на WebView2 — а его под wine нет вовсе (замерено
# 20.08 в windows.sh), и родитель встанет колом на этом шаге, так и не выйдя.
# --tiho — тот же путь до окна включительно (проверка обновления, поиск
# работающей копии, подъём службы, ожидание метки), но без самого окна:
# ровно то место кода, что нужно пощупать.
#
# Что стенд ПОКАЗЫВАЕТ:
#   а) метка копии (zapushcheno.json) появляется, пока родитель ещё работает;
#   б) unix-процесс, которым МЫ запустили --tiho, завершается сам (это и есть
#      «родитель отдал управление, а не завис в ожидании ребёнка»);
#   в) unix-процесс службы (--sluzhba) остаётся ЖИВ и отвечает по HTTP уже
#      ПОСЛЕ того, как родителя не стало.
# Чего стенд НЕ ЗАМЕНЯЕТ (те же ограничения, что у windows.sh):
#   · окно: WebView2 под wine не поднимается, щуп его не проверяет;
#   · реальный DETACHED_PROCESS/CREATE_NEW_PROCESS_GROUP на живой Windows:
#     wine эмулирует CreateProcess по-своему, это не то же самое ядро, что
#     у пользователя. Здесь доказан ЭФФЕКТ (родитель ушёл, ребёнок жив), а не
#     сам системный вызов.
set -u
KORFN=$(cd "$(dirname "$0")/.." && pwd)
WINE=${WINE:-/usr/lib/wine/wine64}
export WINEPREFIX=${WINEPREFIX:-$KORFN/.wine}
export WINEDEBUG=${WINEDEBUG:--all}
export HOME=${HOME:-/root}
export LANG=${LANG:-C.UTF-8} LC_ALL=${LC_ALL:-C.UTF-8}
# Настоящая причина падений wine (TMPDIR не должен быть экспортирован в его
# окружение вообще) и unset для неё — в stend/obshchee.sh, до первого вызова wine.
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

PAPKA="$WINEPREFIX/drive_c/users/$(whoami)/AppData/Local/Kelevra"
ZHURNAL="$PAPKA/kelevra.log"
METKA="$PAPKA/zapushcheno.json"
bed=0

echo "── сборка Kelevra.exe (windows/amd64) ──"
if ! GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui" -o "$STEND/Kelevra.exe" "$KORFN/cmd/kelevra" 2>&1; then
  echo "  НЕ СОБРАЛСЯ"; exit 1
fi

# Чистый след прошлых прогонов: старая метка выглядела бы как «копия уже
# работает» и щуп проверял бы не то.
pkill -f "Kelevra.exe" 2>/dev/null; sleep 1
rm -f "$ZHURNAL" "$METKA"

echo "── запуск --tiho (родитель) ──"
# KELEVRA_BEZ_OBNOVLENIYA=1 — иначе первый шаг тот же, что в windows.sh: без
# него --tiho может уйти проверять обновление в реальный GitHub и подменить
# себя чужим релизом ещё до того, как мы вообще увидим наш код (поймано
# 20.08 в obnovlenie.sh).
wine_zapusti "$STEND/razdvoenie_tiho.log" "$ZHURNAL" - 25 -- \
  env KELEVRA_BEZ_OBNOVLENIYA=1 timeout 30 "$WINE" "$STEND/Kelevra.exe" --tiho
mertv=$?
RODITEL=$WINE_ZAPUSTI_PID
echo "  unix-pid родителя: $RODITEL"
if [ "$mertv" -eq 77 ]; then
  echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
  exit 7
fi

# (б) ждём, пока РОДИТЕЛЬ сам завершится — не убиваем, а именно ждём: если бы
# он завис в ожидании ребёнка (старое поведение — один процесс на двоих), этот
# цикл вычерпал бы весь лимit и ушёл бы в красный по таймауту. wine_zapusti уже
# отждал(а) до 25с или до выхода процесса — здесь только читаем итог.
if kill -0 "$RODITEL" 2>/dev/null; then
  echo "  КРАСНЫЙ: родитель (--tiho) не вышел за 25с — застрял, как в старой схеме «один процесс»"
  bed=1
else
  echo "  зелёный: родитель (--tiho) вышел сам, дальше работает не он"
fi
wait "$RODITEL" 2>/dev/null

# (а) метка копии — её пишет уже РЕБЁНОК (--sluzhba), родитель только читает.
if [ -f "$METKA" ]; then
  adres=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))['url'])" "$METKA" 2>/dev/null)
  echo "  зелёный: метка копии есть, адрес: $adres"
else
  echo "  КРАСНЫЙ: метки копии нет — служба не отметилась"
  bed=1
fi

# (в) unix-процесс службы жив и отвечает — именно ОН, а не какой-то старый
# хвост: cmdline windows-процесса под wine несёт наш аргумент --sluzhba.
sluzhba_pid=$(pgrep -f "Kelevra.exe --sluzhba" | head -1)
if [ -n "$sluzhba_pid" ]; then
  echo "  зелёный: процесс службы жив после выхода родителя, unix-pid $sluzhba_pid"
else
  echo "  КРАСНЫЙ: процесса службы после выхода родителя не видно"
  bed=1
fi

if [ -n "${adres:-}" ]; then
  kod=$(curl -s -o /dev/null -w '%{http_code}' "${adres}api/sostoyanie")
  echo "  GET ${adres}api/sostoyanie -> код $kod"
  if [ "$kod" != "200" ]; then
    echo "  КРАСНЫЙ: служба на метку не отвечает"
    bed=1
  else
    echo "  зелёный: служба живая, отвечает по HTTP уже без родителя"
  fi
fi

echo "── журнал (родитель + ребёнок пишут в один файл) ──"
tail -8 "$ZHURNAL" 2>/dev/null | sed 's/^/  /'

pkill -f "Kelevra.exe" 2>/dev/null

echo "── итог: $([ $bed -eq 0 ] && echo ЗЕЛЁНЫЙ || echo КРАСНЫЙ) ──"
exit $bed
