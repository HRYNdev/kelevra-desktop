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
# Настоящая причина падений wine (TMPDIR не должен быть экспортирован в его
# окружение вообще) и unset для неё — в stend/obshchee.sh, до первого вызова wine.
STEND=$KORFN/.stend_win
mkdir -p "$STEND" "$WINEPREFIX"
. "$KORFN/stend/obshchee.sh"

command -v go >/dev/null 2>&1 || export PATH="$PATH:/usr/local/go/bin"

# AVTOREZHIM_DNS_PODMENA (28.08, #78): «Подключиться» с 988e419 сам решает,
# дома человек или нет (domaSeychas), а этот контейнер живёт за резолвером,
# который на контрольные домены отвечает fake-ip подменой (тот же диапазон
# 198.18.0.0/15, что и у нашего ядра) — ровно как настоящий дом, см.
# stend/zond_doma.sh. Без подмены зонд честно, но ложно решает «дома»,
# podklyuchit молча не поднимает защиту, и все сценарии этого стенда бьют
# мимо продукта. 127.0.0.1:1 никто не слушает — DomaPoDns получает мгновенный
# ECONNREFUSED и честно решает «не дома» (безопасный дефолт, см. Avtorezhim.Zahod).
AVTOREZHIM_DNS_PODMENA="127.0.0.1:1"
# AVTOREZHIM_SHLYUZ_PODMENA: тот же зазор, но по ARP-шлюзу — стенд физически
# стоит внутри домашней сети владельца, поэтому domaSeychas честно видит домашний
# MAC-шлюз и решает «дома» (dom_po_shlyuzu.go). Подставной MAC уводит это
# решение в «не дома», не трогая сам продукт.
AVTOREZHIM_SHLYUZ_PODMENA="de:ad:be:ef:00:01"

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

reg_get() { # $1 имя значения -> печатает значение (или пусто)
  # wine пишет вывод с \r\n (виндовый формат): без tr -d '\r' сравнение строк
  # в скрипте молча ломается — 0x0 с хвостовым CR не равен чистому "0x0".
  "$WINE" reg query "$NASTR" /v "$1" 2>/dev/null | tr -d '\r' | awk -v v="$1" '$1==v {print $3}'
}
# ПУСТОЙ pgrep ≠ ТИХИЙ РЕЕСТР. Убитый SIGKILL'ом Kelevra.exe/ядро успевает
# отправить запись в wineserver, а тот кладёт её в реестр уже ПОСЛЕ того, как
# процесса нет: 29.08 стенд краснел через раз и каждый раз ДРУГИМ сценарием —
# в сц.6 старым оказывался ProxyEnable, в сц.8 ProxyServer, будто продукт пишет
# из двух значений только одно. Замер (9 изолированных прогонов одного писателя,
# чтение с t=0 каждые 0.2с) показал: оба значения всегда меняются вместе, в одном
# чтении. Расщепление приходило только в многосценарном прогоне и только хвостом
# от процесса ПРЕДЫДУЩЕГО сценария. Продукт цел, врала площадка.
zhdat_tishiny_reestra() { # ждём, пока реестр перестанет меняться сам
  proshloe=""; tiho=0
  for _ in $(seq 1 40); do  # предел 20 с
    seychas="$(reg_get ProxyEnable)|$(reg_get ProxyServer)"
    if [ "$seychas" = "$proshloe" ]; then
      tiho=$((tiho + 1)); [ "$tiho" -ge 3 ] && return 0   # 3 одинаковых чтения подряд
    else
      tiho=0
    fi
    proshloe="$seychas"; sleep 0.5
  done
  echo "⚫ ПРИБОР МЁРТВ: реестр не замолкает 20 с (последнее: $proshloe) — площадку не на чем строить"
  exit 7
}
# Выставить площадку. Зовётся ТОЛЬКО до того, как продукт что-то сделал: чтения
# ПОСЛЕ действия продукта не трогаем ничем, иначе лекарство ослепит прибор.
reg_set() { # $1 ProxyEnable(0|1)  $2 ProxyServer
  zhdat_tishiny_reestra
  for popytka in 1 2 3; do
    "$WINE" reg add "$NASTR" /v ProxyEnable /t REG_DWORD /d "$1" /f >/dev/null 2>&1
    "$WINE" reg add "$NASTR" /v ProxyServer /t REG_SZ /d "$2" /f >/dev/null 2>&1
    sleep 1   # хвост чужой записи, если он ещё в пути, приезжает здесь
    if [ "$(reg_get ProxyEnable)" = "0x$1" ] && [ "$(reg_get ProxyServer)" = "$2" ]; then
      return 0
    fi
    echo "  ── площадку сдуло чужим хвостом (попытка $popytka: ProxyEnable=$(reg_get ProxyEnable) ProxyServer=$(reg_get ProxyServer)), выставляю заново"
    zhdat_tishiny_reestra
  done
  echo "⚫ ПРИБОР МЁРТВ: площадку не удержать — реестр перебивает кто-то живой"
  exit 7
}

# запускает Kelevra.exe в служебном режиме, возвращает URL службы через echo.
# Возврат функции 77 (через $?) значит «прибор мёртв» — см. stend/obshchee.sh.
zapustit_i_vzyat_url() { # $1 = файл для журнала запуска
  # KELEVRA_BEZ_OBNOVLENIYA=1 — иначе версия по умолчанию (0.1.0-rabota) видит
  # на GitHub релиз новее, тихо подменяет себя настоящим релизом и
  # перезапускается ИМ: стенд честно проверял бы чужой бинарь без нашей правки
  # (поймано 20.08: журнал показал "обновление: вышла версия 0.5.1, ставлю").
  wine_zapusti "$1" "$ZHURNAL" "служба слушает" 20 -- \
    env KELEVRA_BEZ_OKNA=1 KELEVRA_BEZ_OBNOVLENIYA=1 KELEVRA_AVTOREZHIM_DNS="$AVTOREZHIM_DNS_PODMENA" KELEVRA_AVTOREZHIM_SHLYUZ="$AVTOREZHIM_SHLYUZ_PODMENA" \
    timeout 30 "$WINE" "$STEND/Kelevra.exe"
  local rc=$?
  grep -o 'http://[^ ]*' "$ZHURNAL" 2>/dev/null | tail -1
  return "$rc"
}

ostanovit() { # гасит процесс стенда по короткому имени: /proc/PID/cmdline у
  # wine-процесса — виндовый путь "Z:\...\Kelevra.exe" (обратные слэши, без
  # $STEND), полный POSIX-путь в нём никогда не совпадёт. SIGTERM не всегда
  # доходит до эмулированного Windows-процесса, поэтому ждём и добиваем SIGKILL.
  # Гасим и ЯДРО: сценарий 4 поднимает настоящий sing-box.exe, а он — отдельный
  # процесс, переживающий смерть Kelevra.exe. 23.08 переживший ядро держал порт
  # 2412 и ронял соседние стенды в общей приёмке (proksi.sh зелёный в одиночку,
  # красный внутри vse.sh) — беда была не в продукте, а в этой уборке.
  pkill -TERM -f '[K]elevra\.exe' 2>/dev/null
  pkill -TERM -f '[s]ing-box\.exe' 2>/dev/null
  for _ in $(seq 1 10); do
    pgrep -f '[K]elevra\.exe|[s]ing-box\.exe' >/dev/null 2>&1 || return 0
    sleep 1
  done
  pkill -KILL -f '[K]elevra\.exe' 2>/dev/null
  pkill -KILL -f '[s]ing-box\.exe' 2>/dev/null
  sleep 1
}

echo "── сборка Kelevra.exe (windows/amd64) ──"
if ! GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui" -o "$STEND/Kelevra.exe" "$KORFN/cmd/kelevra" 2>&1; then
  echo "  НЕ СОБРАЛСЯ"; exit 1
fi

# Лже-ядро нужно сц.5/8 ниже — но теперь уже не как подстраховка (её физически
# нет), а только как ВХОД: тот же бинарь, что раньше изображал два разных
# отказа, подкладываем и сейчас, чтобы вход сценариев не изменился.
echo "── сборка лже-ядра для сценариев 5/8 (cmd/lzhe_yadro, windows/amd64) ──"
if ! GOOS=windows GOARCH=amd64 go build -o "$STEND/lzhe_yadro.exe" "$KORFN/cmd/lzhe_yadro" 2>&1; then
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
  elif [ -n "$server_posle" ]; then
    # 0.6.77: UbratSled стирает строку адреса целиком (DeleteValue), а не
    # просто гасит галочку — старое приложение оставляло ProxyServer висеть
    # нетронутым, это и была дыра 31.08 (врала при разборе аварии).
    echo "  КРАСНЫЙ: ProxyServer не стёрт (осталось $server_posle)"; bed=1
  else
    echo "  зелёный: прокси снят, строка адреса стёрта"
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

# Сценарии 4/5/8 до e36411b (17.10) доказывали ПОСТАНОВКУ системного прокси
# тремя разными входами (успешная постановка ядром, подстраховка приложением
# после громкого отказа ядра, подстраховка после ТИХОГО отказа) — ровно по
# числу путей, которыми Postavit() мог сработать. С 17.10 Postavit() из кода
# физически убран (internal/proksi больше не умеет ставить прокси никому), и
# права проверяются РАНЬШЕ сборки конфига и запуска ядра (estPrava() в самом
# начале PodnyatZashchitu, internal/sluzhba.go) — у этой песочницы прав нет,
# значит все три входа теперь обрываются в ОДНОЙ и той же точке, до того как
# код ядра/лже-ядра вообще успевает отличиться. Отличать их по-прежнему, но
# ждать от каждого РАЗНОЕ — значило бы судить код, которого больше нет.
# Поэтому вход у каждого сценария прежний (тот же профиль, то же «чужой
# выключенный прокси» в реестре до запуска), а проверка теперь ОДНА и та же
# на все три: «Подключить» без прав обязан провалиться СЛОВАМИ про права, не
# тронув реестр вовсе — постановки прокси нет ни при каком из трёх входов.
#
# «mixed-in вырезан из конфига» проверяем ОДИН раз (а не три): это чистая
# функция от профиля (konfig.Prigotovit), ядро сюда вообще не вовлечено, и
# гонять один и тот же профиль через неё трижды добавило бы не покрытие, а
# шум. Берём cmd/zamer_konfig — тот же код приложения, не самодельный разбор.
echo "── общая проверка для сц.4/5/8: конфиг для ядра не содержит локальный вход (mixed/http/socks) — Postavit() нечему ставить ──"
VHODY=$(go run "$KORFN/cmd/zamer_konfig" -profil "$KORFN/internal/konfig/testdata/profil_stend_bez_seti.json" 2>/dev/null \
  | python3 -c '
import json, sys
d = json.load(sys.stdin)
print(",".join(sorted(v.get("type", "?") for v in d.get("inbounds", []))))
')
if [ -z "$VHODY" ]; then
  echo "⚫ ПРИБОР МЁРТВ: cmd/zamer_konfig не собрал конфиг — проверить, что в нём, нечем"
  exit 7
fi
if printf '%s' "$VHODY" | grep -qE 'mixed|http|socks'; then
  echo "  КРАСНЫЙ: в готовом конфиге всё равно есть локальный вход ($VHODY) — прокси-режим вернулся"; bed=1
else
  echo "  зелёный: входы готового конфига — только $VHODY, локального прокси-входа нет"
fi

# bez_prav_odin_i_tot_zhe_itog <номер> <что лежит в YADRO_PAPKA/sing-box.exe
# ДО запуска, человеческими словами> — общее тело сц.4/5/8: ставит площадку
# (профиль + «чужой выключенный» реестр), жмёт «Подключить» без прав и судит
# ОДИНАКОВО для всех трёх. $1 правит только имена файлов лога, чтобы прогоны
# не затирали друг друга.
bez_prav_odin_i_tot_zhe_itog() {
  local nomer=$1 opisanie=$2
  reg_set 0 10.0.0.9:9999
  local do_before do_server_before
  do_before=$(reg_get ProxyEnable); do_server_before=$(reg_get ProxyServer)
  echo "  до: ProxyEnable=$do_before ProxyServer=$do_server_before (чужой выключенный прокси; в ядре: $opisanie)"
  local url
  url=$(zapustit_i_vzyat_url "$STEND/proksi_zapusk${nomer}.log")
  if [ "$?" -eq 77 ]; then
    echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
    exit 7
  fi
  if [ -z "$url" ]; then
    echo "  служба не поднялась, журнал:"; tail -8 "$ZHURNAL" 2>/dev/null; bed=1
    return
  fi
  echo "  служба: $url"
  local otvet posle server_posle
  otvet=$(curl -s -m 30 "${url}api/podklyuchit")
  echo "  POST podklyuchit -> $otvet"
  sleep 1
  posle=$(reg_get ProxyEnable); server_posle=$(reg_get ProxyServer)
  echo "  после: ProxyEnable=$posle ProxyServer=$server_posle"
  if ! printf '%s' "$otvet" | grep -q 'права администратора'; then
    echo "  КРАСНЫЙ: отказ не про права ($otvet) — ждали OshibkaNetPrav"; bed=1
  elif [ "$posle" != "$do_before" ] || [ "$server_posle" != "$do_server_before" ]; then
    echo "  КРАСНЫЙ: реестр тронут без прав (было $do_before/$do_server_before, стало $posle/$server_posle)"; bed=1
  else
    echo "  зелёный: без прав — отказ словами про права, реестр не тронут, прокси не поставлен"
  fi
}

YADRO_ISTOCHNIK=$KORFN/.stend_win/sing-box.exe
if [ ! -s "$YADRO_ISTOCHNIK" ]; then
  echo "⚫ ПРИБОР МЁРТВ: нет настоящего ядра ($YADRO_ISTOCHNIK) — вход сценария подложить нечем"
  exit 7
fi
# ostanovit_zhestko — в сц.4/5/8 podklyuchit падает на правах ДО того, как
# код коснётся ядра; процесс жив и держит порт дальше. Обычный ostanovit()
# гасит его SIGTERM'ом сначала — а SIGTERM здесь уводит процесс в штатный
# выход (rabotaSluzhby, ветка sTreem), который сам, безусловно, зовёт
# proksi.UbratSled()/tunnel.UbratMetku() при ЛЮБОМ завершении, не только
# после настоящего подключения. bash видит процесс мёртвым раньше, чем
# wineserver донесёт этот вызов до диска/реестра, и хвост приезжает уже в
# сц.6 — тем самым race'ом, что в zhdat_tishiny_reestra, только теперь по
# файлу метки, а не по реестру (пойман здесь же, 18.10). Раз подключения не
# было и нечего штатно сворачивать — глушим сразу SIGKILL, без шанса на
# хвост.
ostanovit_zhestko() {
  pkill -KILL -f '[K]elevra\.exe' 2>/dev/null
  pkill -KILL -f '[s]ing-box\.exe' 2>/dev/null
  sleep 1
}

echo "── сценарий 4 (бывш. «удачное подключить ставит прокси»): настоящее ядро, вход тот же — постановки больше нет ──"
cp "$KORFN/internal/konfig/testdata/profil_stend_bez_seti.json" "$PROFIL"
cp "$YADRO_ISTOCHNIK" "$YADRO_PAPKA/sing-box.exe"
bez_prav_odin_i_tot_zhe_itog 4 "настоящее sing-box.exe"
ostanovit_zhestko

echo "── сценарий 5 (бывш. «подстраховка после громкого отказа ядра»): лже-ядро, вход тот же — подстраховки ставить нечего ──"
cp "$STEND/lzhe_yadro.exe" "$YADRO_PAPKA/sing-box.exe"
bez_prav_odin_i_tot_zhe_itog 5 "лже-ядро (имитирует отказ «system proxy»)"
ostanovit_zhestko

echo "── сценарий 8 (бывш. «подстраховка после тихого отказа ядра»): лже-ядро с tiho.marker, вход тот же ──"
: > "$YADRO_PAPKA/tiho.marker"
bez_prav_odin_i_tot_zhe_itog 8 "лже-ядро (имитирует тихий успех без прокси)"
rm -f "$YADRO_PAPKA/tiho.marker"
ostanovit_zhestko

echo "── сценарий 6: след СТАРОЙ версии (до 0.6.77) переживший жёсткую смерть — обязан снять СЛЕДУЮЩИЙ запуск окна ──"
# До 0.6.77 ядро само ставило системный прокси, и жёсткая смерть (Диспетчер
# задач, пропадание питания — оконная сборка без консоли SIGTERM не получает)
# оставляла его висеть (жалоба 20.08 10:23). С 0.6.77 этот путь ПОСТАНОВКИ
# больше не существует вовсе (см. правку e36411b, konfig.go: set_system_proxy
# не ставится никогда) — живьём воспроизвести «ядро поставило прокси и умерло»
# нечем: сборка уже не умеет его ставить, а без прав администратора (их у
# этого wine-процесса в этой песочнице нет и не будет) до запуска ядра дело
# вообще не доходит.
#
# Проверка, которую это ломает, НЕ про постановку — она про УБОРКУ следа
# версии, обновившейся ИЗ прокси-режима (ровно задача proksi.UbratSled,
# см. его шапку). Этот след сюда кладём руками, как кладёт его старая
# версия: реестр + файл-метка, — а не через живое «Подключить», которого
# больше нет. Служба (--sluzhba) запускается, но без podklyuchit: UbratSled
# в сервисной сборке зовётся только на подъёме туннеля и на выходе
# (cmd/kelevra/main.go), поэтому голая служба ничего не трогает, а SIGKILL
# снимает её, как Диспетчер задач — без единого штатного выхода.
METKA="$WINEPREFIX/drive_c/users/$(whoami)/AppData/Local/Kelevra/proksi.json"
mkdir -p "$(dirname "$METKA")"
# Площадка здесь — та же папка AppData, что и у сц.4/5/8 перед этим: их
# завершение (ostanovit → SIGTERM) идёт через ШТАТНЫЙ выход процесса, а он
# сам зовёт proksi.UbratSled()+tunnel.UbratMetku() (cmd/kelevra/main.go,
# rabotaSluzhby, ветка sTreem) — то есть вайп метки и реестра на их СВОЁМ
# выходе предусмотрен кодом, а не брак площадки. Беда в ДРУГОМ: bash видит
# процесс мёртвым (pgrep молчит) раньше, чем wineserver успевает донести
# этот вайп до диска/реестра — хвост приезжает уже ПОСЛЕ "до:" этого
# сценария и стирает то, что мы только что положили руками (тот же класс
# гонки, что в zhdat_tishiny_reestra выше, только по файлу, не по реестру).
# Пишем метку и ждём секунду: не пережила — хвост ещё в пути, перекладываем снова.
for popytka in 1 2 3; do
  rm -f "$METKA"
  reg_set 1 127.0.0.1:2412
  printf '{"adres":"127.0.0.1:2412"}' > "$METKA"
  sleep 1
  if [ -s "$METKA" ] && [ "$(reg_get ProxyEnable)" = "0x1" ] && [ "$(reg_get ProxyServer)" = "127.0.0.1:2412" ]; then
    break
  fi
  echo "  ── площадку сдуло чужим хвостом (попытка $popytka), выставляю заново"
  [ "$popytka" -eq 3 ] && { echo "⚫ ПРИБОР МЁРТВ: площадку не удержать — хвост предыдущего сценария перебивает метку"; exit 7; }
done
echo "  до: ProxyEnable=$(reg_get ProxyEnable) ProxyServer=$(reg_get ProxyServer), метка: $(cat "$METKA")  (след старой версии, руками)"
url=$(zapustit_i_vzyat_url "$STEND/proksi_zapusk6.log")
if [ "$?" -eq 77 ]; then
  echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
  exit 7
fi
if [ -z "$url" ]; then
  echo "  служба не поднялась, журнал:"; tail -8 "$ZHURNAL" 2>/dev/null; bed=1
else
  echo "  служба: $url (жду, трогаю её только SIGKILL — ни api/podklyuchit, ни штатного выхода)"
  echo "  убиваю Kelevra.exe жёстко (kill -9, без выхода)"
  pkill -KILL -f '[K]elevra\.exe' 2>/dev/null
  sleep 1
  posle_kill=$(reg_get ProxyEnable); server_posle_kill=$(reg_get ProxyServer)
  echo "  после жёсткой смерти: ProxyEnable=$posle_kill ProxyServer=$server_posle_kill, метка: $([ -s "$METKA" ] && cat "$METKA" || echo 'НЕТ')"
  if [ "$posle_kill" != "0x1" ]; then
    echo "  КРАСНЫЙ: реестр сам расчистился без нашей починки — сценарий не воспроизвёл беду"; bed=1
  elif [ ! -s "$METKA" ]; then
    echo "  КРАСНЫЙ: метка пропала сама по себе — сценарий не воспроизвёл беду"; bed=1
  else
    echo "  подтверждено: ProxyEnable=0x1 висит без службы за ним — след старой версии воспроизведён"
    echo "  запускаю Kelevra.exe СНОВА, в режиме окна (--tiho, без --sluzhba)"
    wine_zapusti "$STEND/proksi_zapusk6_okno.log" "$ZHURNAL" "--- запуск Kelevra" 25 -- \
      env KELEVRA_BEZ_OBNOVLENIYA=1 KELEVRA_AVTOREZHIM_DNS="$AVTOREZHIM_DNS_PODMENA" KELEVRA_AVTOREZHIM_SHLYUZ="$AVTOREZHIM_SHLYUZ_PODMENA" \
      timeout 30 "$WINE" "$STEND/Kelevra.exe" --tiho
    rc=$?
    if [ "$rc" -eq 77 ]; then
      echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
      exit 7
    fi
    sleep 2
    itog=$(reg_get ProxyEnable); server_itog=$(reg_get ProxyServer)
    echo "  после повторного запуска окна: ProxyEnable=$itog ProxyServer=$server_itog, метка: $([ -s "$METKA" ] && cat "$METKA" || echo 'НЕТ')"
    if [ "$itog" != "0x0" ]; then
      echo "  КРАСНЫЙ: ProxyEnable не снят повторным запуском окна (осталось $itog)"; bed=1
    elif [ -n "$server_itog" ]; then
      echo "  КРАСНЫЙ: ProxyServer не стёрт повторным запуском окна (осталось $server_itog)"; bed=1
    elif [ -s "$METKA" ]; then
      echo "  КРАСНЫЙ: прокси сняли, а метка на диске осталась — следующий запуск снова полезет её проверять зря"; bed=1
    else
      echo "  зелёный: след старой версии снят повторным запуском окна, метка убрана"
    fi
  fi
fi
ostanovit

echo "── сценарий 7 (страховка): чужой прокси без нашей метки повторный запуск окна не трогает ──"
# Симметричная проверка сценарию 6: если метки на диске нет вовсе (мы прокси
# не ставили — это прокси человека, прописанный им самим руками или другой
# программой), окно не имеет права его снимать, даже если живой копии службы
# тоже нет. Без этой проверки починка сценария 6 была бы неотличима от «окно
# снимает любой чужой прокси, раз службы не видно» — а это ровно то, чего
# делать нельзя.
rm -f "$METKA"
reg_set 1 10.0.0.9:9999
echo "  до: ProxyEnable=$(reg_get ProxyEnable) ProxyServer=$(reg_get ProxyServer) (чужой ВКЛЮЧЁННЫЙ прокси, метки точно нет)"
wine_zapusti "$STEND/proksi_zapusk7_okno.log" "$ZHURNAL" "--- запуск Kelevra" 15 -- \
  env KELEVRA_BEZ_OBNOVLENIYA=1 KELEVRA_AVTOREZHIM_DNS="$AVTOREZHIM_DNS_PODMENA" KELEVRA_AVTOREZHIM_SHLYUZ="$AVTOREZHIM_SHLYUZ_PODMENA" \
  timeout 30 "$WINE" "$STEND/Kelevra.exe" --tiho
rc=$?
if [ "$rc" -eq 77 ]; then
  echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
  exit 7
fi
sleep 2
posle=$(reg_get ProxyEnable); server_posle=$(reg_get ProxyServer)
echo "  после запуска окна: ProxyEnable=$posle ProxyServer=$server_posle, метка: $([ -s "$METKA" ] && cat "$METKA" || echo 'НЕТ')"
if [ "$posle" != "0x1" ]; then
  echo "  КРАСНЫЙ: чужой ProxyEnable тронут без метки (стало $posle)"; bed=1
elif [ "$server_posle" != "10.0.0.9:9999" ]; then
  echo "  КРАСНЫЙ: чужой ProxyServer тронут без метки (стало $server_posle)"; bed=1
else
  echo "  зелёный: чужой включённый прокси без нашей метки не тронут"
fi
ostanovit

echo "── итог: $([ $bed -eq 0 ] && echo ЗЕЛЁНЫЙ || echo КРАСНЫЙ) ──"
exit $bed
