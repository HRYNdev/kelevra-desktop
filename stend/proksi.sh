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

echo "── сборка лже-ядра для сценария 5 (cmd/lzhe_yadro, windows/amd64) ──"
if ! GOOS=windows GOARCH=amd64 go build -o "$STEND/lzhe_yadro.exe" "$KORFN/cmd/lzhe_yadro" 2>&1; then
  echo "  НЕ СОБРАЛСЯ"; exit 1
fi

# ozhidaemyy_adres_proksi <profil.json> — печатает host:port первого входа
# mixed/http/socks, ровно так же, как internal/konfig.adresVhoda его строит.
# Приговор сценариев 4 и 5 сверяет реестр с этим адресом, а не с тем, что о
# себе рассказывает само приложение через /api/sostoyanie: та ручка вообще
# не отдаёт proksi_adres (otvetSostoyaniya в internal/sluzhba/sluzhba.go), и
# полагаться на самоотчёт проверяемой стороны — не проверка, а эхо.
ozhidaemyy_adres_proksi() {
  python3 - "$1" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
for v in d.get("inbounds", []):
    if v.get("type") in ("mixed", "http", "socks"):
        host = v.get("listen") or "127.0.0.1"
        port = v.get("listen_port")
        if port:
            print(f"{host}:{port}")
            break
PY
}

# ruchnoy_proksi_iz_sostoyaniya <json ответ /api/sostoyanie> — печатает
# True/False. RuchnoyProksi в otvetSostoyaniya несёт `json:",omitempty"`,
# значит при false поле вообще ПРОПАДАЕТ из ответа — голый .get() без
# значения по умолчанию печатал бы None и приговор бы не срабатывал.
ruchnoy_proksi_iz_sostoyaniya() {
  python3 -c 'import json,sys
try:
    d=json.load(sys.stdin)
except Exception:
    print("False"); raise SystemExit
print(bool(d.get("ruchnoy_proksi", False)))'
}

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

echo "── сценарий 4: удачное «Подключить» обязано ПОСТАВИТЬ системный прокси, а не соврать про ручную правку ──"
# Дыра, найденная 23.08 по жалобе хозяина (22.08 22:08): «включил впн твой, ииии
# нихуя, пошел подумал включил САМ прокси на ПК, и *** заработало». Сценарии
# 1-3 проверяют только СНЯТИЕ прокси. Постановку не проверял никто: её делает
# чужой код (ядро, по set_system_proxy в конфиге), а PodnyatZashchitu после
# успешного Zapustit() не смотрел в реестр вовсе — окно зеленело, трафик мог
# идти мимо.
#
# ПОЧЕМУ ПРЕЖНИЙ ПРИГОВОР ЭТОГО СЦЕНАРИЯ БЫЛ НЕПРАВ. Замер 23.08, дословно:
#   до: ProxyEnable=0x0 ProxyServer=10.0.0.9:9999 (чужой выключенный прокси)
#   после: ProxyEnable=0x1 ProxyServer=http://127.0.0.1:2412
#   картина: proksi_adres= ruchnoy_proksi=True
# Реестр реально встал (ProxyEnable=0x1, наш адрес) — ядро успело прописать
# его само ДО того, как упасть на notify-вызове (winapi error #12009), и
# приложение ушло в подстраховку BezSistemnogoProksi. А приложение при этом
# говорило человеку в окне «Windows не дал включить защиту сам, впишите
# адрес руками» — старый приговор ниже засчитывал именно это как
# «зелёный по договору». Раз реестр уже правильный — врать об этом нельзя,
# это и есть ровно та дыра, на которую хозяин указал 23.08 15:36: «я видел
# рабочие vpn клиенты, хули ты мне тут затираешь ваще что невозможно».
# Приговор теперь судит правду в реестре И правдивость записки одновременно.
YADRO_ISTOCHNIK=$KORFN/.stend_win/sing-box.exe
if [ ! -s "$YADRO_ISTOCHNIK" ]; then
  echo "⚫ ПРИБОР МЁРТВ: нет настоящего ядра ($YADRO_ISTOCHNIK) — постановку прокси проверить нечем"
  exit 7
fi
# Профиль тут НЕ боевой телефонный, и это осознанно: в боевом 22 удалённых
# rule_set, ядро тянет их с subkv.chickenkiller.com ПЕРЕД стартом и без сети
# падает FATAL — стенд мерил бы связь LXC, а не постановку прокси. Здесь
# профиль-близнец без сетевых rule_set (сгенерирован из боевого), всё
# остальное — настоящее: то же ядро sing-box.exe, тот же путь приложения.
cp "$KORFN/internal/konfig/testdata/profil_stend_bez_seti.json" "$PROFIL"
cp "$YADRO_ISTOCHNIK" "$YADRO_PAPKA/sing-box.exe"
ADRES_OZHIDAEMYY=$(ozhidaemyy_adres_proksi "$PROFIL")
echo "  ожидаемый адрес прокси из профиля: $ADRES_OZHIDAEMYY"
reg_set 0 10.0.0.9:9999
echo "  до: ProxyEnable=$(reg_get ProxyEnable) ProxyServer=$(reg_get ProxyServer) (чужой выключенный прокси)"
url=$(zapustit_i_vzyat_url "$STEND/proksi_zapusk4.log")
if [ "$?" -eq 77 ]; then
  echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
  exit 7
fi
if [ -z "$url" ]; then
  echo "  служба не поднялась, журнал:"; tail -8 "$ZHURNAL" 2>/dev/null; bed=1
else
  echo "  служба: $url"
  otvet=$(curl -s -m 120 "${url}api/podklyuchit")
  echo "  POST podklyuchit -> $otvet"
  sost=$(curl -s -m 10 "${url}api/sostoyanie")
  ruchnoy=$(printf '%s' "$sost" | ruchnoy_proksi_iz_sostoyaniya)
  sleep 2
  posle=$(reg_get ProxyEnable); server_posle=$(reg_get ProxyServer)
  echo "  после: ProxyEnable=$posle ProxyServer=$server_posle ruchnoy_proksi=$ruchnoy"
  if printf '%s' "$otvet" | grep -q '"beda"'; then
    echo "  КРАСНЫЙ: подключение не удалось — постановку прокси проверить не на чем"
    tail -12 "$YADRO_PAPKA/yadro.log" 2>/dev/null; bed=1
  elif [ "$posle" != "0x1" ]; then
    echo "  КРАСНЫЙ: подключились, окно зелёное, а ProxyEnable=$posle — трафик идёт мимо защиты"; bed=1
  elif ! printf '%s' "$server_posle" | grep -qF "$ADRES_OZHIDAEMYY"; then
    echo "  КРАСНЫЙ: ProxyServer=$server_posle не содержит ожидаемый адрес $ADRES_OZHIDAEMYY"; bed=1
  elif [ "$ruchnoy" = "True" ]; then
    echo "  КРАСНЫЙ: реестр стоит правильно ($server_posle), а приложение всё равно врёт запиской «впишите вручную» (ruchnoy_proksi=true)"; bed=1
  else
    echo "  зелёный: прокси реально поставлен ($server_posle), записки про ручную правку нет"
  fi
fi
ostanovit

echo "── сценарий 5: подстраховка ядра не смогла поставить прокси сама — приложение обязано прописать реестр вместо неё ──"
# Сценарий 4 доказывает интеграцию целиком настоящим ядром, но под этим wine
# оно на первой (неудачной) попытке успевает ЗАПИСАТЬ реестр ДО того, как
# упасть на notify-вызове — значит internal/proksi.Stoit() там уже находит
# правильную запись, и ветка proksi.Postavit() (реестр правит само
# приложение, а не ядро) живьём не проверяется ни разу. Лже-ядро
# (cmd/lzhe_yadro) разыгрывает противоположный, тоже возможный на живой
# Windows случай: первая попытка падает строкой «system proxy», реестра не
# коснувшись вовсе (mekhanizm cmd/lzhe_yadro/main.go). После восстановления
# (BezSistemnogoProksi=true, вторая попытка отвечает как обычное ядро)
# реестр обязано прописать САМО приложение — то есть до подключения он
# ровно такой же, каким его оставил стенд (чужой прокси, выключен).
rm -f "$YADRO_PAPKA/popytka.marker"
cp "$STEND/lzhe_yadro.exe" "$YADRO_PAPKA/sing-box.exe"
reg_set 0 10.0.0.9:9999
echo "  до: ProxyEnable=$(reg_get ProxyEnable) ProxyServer=$(reg_get ProxyServer) (чужой выключенный прокси)"
url=$(zapustit_i_vzyat_url "$STEND/proksi_zapusk5.log")
if [ "$?" -eq 77 ]; then
  echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
  exit 7
fi
if [ -z "$url" ]; then
  echo "  служба не поднялась, журнал:"; tail -8 "$ZHURNAL" 2>/dev/null; bed=1
else
  echo "  служба: $url"
  otvet=$(curl -s -m 120 "${url}api/podklyuchit")
  echo "  POST podklyuchit -> $otvet"
  sost=$(curl -s -m 10 "${url}api/sostoyanie")
  ruchnoy=$(printf '%s' "$sost" | ruchnoy_proksi_iz_sostoyaniya)
  sleep 2
  posle=$(reg_get ProxyEnable); server_posle=$(reg_get ProxyServer)
  echo "  после: ProxyEnable=$posle ProxyServer=$server_posle ruchnoy_proksi=$ruchnoy"
  if printf '%s' "$otvet" | grep -q '"beda"'; then
    echo "  КРАСНЫЙ: подключение не удалось — постановку прокси приложением проверить не на чем"
    tail -12 "$YADRO_PAPKA/yadro.log" 2>/dev/null; bed=1
  elif [ "$posle" != "0x1" ]; then
    echo "  КРАСНЫЙ: лже-ядро реестр не трогало, приложение тоже не прописало — ProxyEnable=$posle"; bed=1
  elif ! printf '%s' "$server_posle" | grep -qF "$ADRES_OZHIDAEMYY"; then
    echo "  КРАСНЫЙ: ProxyServer=$server_posle не содержит ожидаемый адрес $ADRES_OZHIDAEMYY — прописал не то"; bed=1
  elif [ "$ruchnoy" = "True" ]; then
    echo "  КРАСНЫЙ: реестр приложение прописало само правильно ($server_posle), а записка про ручную правку всё равно висит"; bed=1
  else
    echo "  зелёный: ядро реестр не трогало (лже-ядро), приложение прописало его САМО ($server_posle)"
  fi
fi
ostanovit

echo "── сценарий 6: жёсткая смерть службы (kill -9, без сигнала выхода) — прокси обязан снять СЛЕДУЮЩИЙ запуск окна ──"
# Диагноз 23.08 (третий заход): все мягкие пути (штатный выход, паника,
# «Отключить», неудачный старт ядра) снимают прокси сами. Не накрыт ЖЁСТКИЙ
# путь — Диспетчер задач, выключение/перезагрузка Windows, пропадание питания:
# оконная сборка без консоли не получает SIGTERM, proksi.Snyat() в конце
# zapustitSluzhbu просто не успевает отработать. Ровно жалоба хозяина 20.08 10:23,
# только не для закрытия приложения (уже вылечено), а для смерти без выхода.
#
# Этот сценарий убивает службу SIGKILL (никакого SIGTERM, никакого ожидания —
# так умирает процесс от Диспетчера задач), проверяет, что реестр остался
# висеть, а потом запускает .exe ещё раз в РЕЖИМЕ ОКНА (не --sluzhba) — именно
# там, в cmd/kelevra/main.go, живёт починка. WebView2 под wine не поднимается
# (см. шапку windows.sh), поэтому зовём с --tiho: окно не рисуем, а починка
# снятия прокси срабатывает до него, в самом начале main().
METKA="$WINEPREFIX/drive_c/users/$(whoami)/AppData/Local/Kelevra/proksi.json"
if [ ! -s "$YADRO_ISTOCHNIK" ]; then
  echo "⚫ ПРИБОР МЁРТВ: нет настоящего ядра ($YADRO_ISTOCHNIK) — сценарий нечем ставить"
  exit 7
fi
cp "$KORFN/internal/konfig/testdata/profil_stend_bez_seti.json" "$PROFIL"
cp "$YADRO_ISTOCHNIK" "$YADRO_PAPKA/sing-box.exe"
rm -f "$METKA"
reg_set 0 10.0.0.9:9999
echo "  до: ProxyEnable=$(reg_get ProxyEnable) ProxyServer=$(reg_get ProxyServer) (чужой выключенный прокси, метки нет)"
url=$(zapustit_i_vzyat_url "$STEND/proksi_zapusk6.log")
if [ "$?" -eq 77 ]; then
  echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
  exit 7
fi
if [ -z "$url" ]; then
  echo "  служба не поднялась, журнал:"; tail -8 "$ZHURNAL" 2>/dev/null; bed=1
else
  echo "  служба: $url"
  otvet=$(curl -s -m 120 "${url}api/podklyuchit")
  echo "  POST podklyuchit -> $otvet"
  sleep 2
  posle=$(reg_get ProxyEnable); server_posle=$(reg_get ProxyServer)
  echo "  после «Подключить»: ProxyEnable=$posle ProxyServer=$server_posle, метка: $([ -s "$METKA" ] && cat "$METKA" || echo 'НЕТ')"
  if printf '%s' "$otvet" | grep -q '"beda"'; then
    echo "  КРАСНЫЙ: подключение не удалось — сценарий нечем ставить"; bed=1
  elif [ "$posle" != "0x1" ] || ! printf '%s' "$server_posle" | grep -qF "$ADRES_OZHIDAEMYY"; then
    echo "  КРАСНЫЙ: прокси не встал как надо ($posle/$server_posle) — сценарий нечем ставить"; bed=1
  elif [ ! -s "$METKA" ]; then
    echo "  КРАСНЫЙ: прокси встал, а метка на диске не записана (проверить нечем п.2 диагноза)"; bed=1
  else
    # Убиваем ТОЛЬКО Kelevra.exe, жёстко и сразу — никакого SIGTERM и ожидания
    # (ostanovit() ниже так умеет, но это мягкая смерть, которую служба уже
    # ловит; здесь нужна именно жёсткая). sing-box.exe специально не трогаем:
    # Диспетчер задач в реальности снимает процесс, который ему указали, а
    # осиротевшее ядро продолжает жить точно так же, как настоящее.
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
      echo "  подтверждено: ProxyEnable=0x1 висит без службы за ним — беда 20.08 без закрытия окна воспроизведена"
      echo "  запускаю Kelevra.exe СНОВА, в режиме окна (--tiho, без --sluzhba)"
      wine_zapusti "$STEND/proksi_zapusk6_okno.log" "$ZHURNAL" "прошлый запуск умер жёстко" 25 -- \
        env KELEVRA_BEZ_OBNOVLENIYA=1 timeout 30 "$WINE" "$STEND/Kelevra.exe" --tiho
      rc=$?
      if [ "$rc" -eq 77 ]; then
        echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
        exit 7
      fi
      sleep 2
      itog=$(reg_get ProxyEnable); server_itog=$(reg_get ProxyServer)
      echo "  журнал (хвост):"; tail -6 "$ZHURNAL" 2>/dev/null
      echo "  после повторного запуска окна: ProxyEnable=$itog ProxyServer=$server_itog, метка: $([ -s "$METKA" ] && cat "$METKA" || echo 'НЕТ')"
      if ! grep -q "прошлый запуск умер жёстко" "$ZHURNAL" 2>/dev/null; then
        echo "  КРАСНЫЙ: окно не заметило осиротевший прокси — строки починки в журнале нет"; bed=1
      elif [ "$itog" != "0x0" ]; then
        echo "  КРАСНЫЙ: ProxyEnable не снят повторным запуском окна (осталось $itog)"; bed=1
      elif [ -s "$METKA" ]; then
        echo "  КРАСНЫЙ: прокси сняли, а метка на диске осталась — следующий запуск снова полезет её проверять зря"; bed=1
      else
        echo "  зелёный: осиротевший прокси снят повторным запуском окна, метка убрана"
      fi
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
  env KELEVRA_BEZ_OBNOVLENIYA=1 timeout 30 "$WINE" "$STEND/Kelevra.exe" --tiho
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

echo "── сценарий 8: ядро стартовало БЕЗ ошибки, но прокси в реестр не прописало — приложение обязано поймать это само ──"
# Дыра, найденная 23.08 вечером. Сценарии 4 и 5 оба входят через ГРОМКИЙ отказ
# ядра: строку «system proxy» в ошибке старта. Проверка реестра
# (proksi.Stoit/Postavit) висела ровно внутри этой ветки — то есть работала
# только тогда, когда ядро само призналось. Тихий отказ (ядро поднялось, порт
# слушает, ошибки нет, а системного прокси в реестре нет) не ловил никто:
# служба отвечала «готово», окно красило защиту зелёным, метка «прокси
# поставили мы» писалась по предположению — а трафик человека шёл мимо туннеля
# и он видел ровно то же, что 20.08: «интернет как будто без VPN».
# Лже-ядро с маркером tiho.marker играет этот случай: успех с первой попытки,
# реестра не касается.
rm -f "$YADRO_PAPKA/popytka.marker" "$METKA"
: > "$YADRO_PAPKA/tiho.marker"
cp "$KORFN/internal/konfig/testdata/profil_stend_bez_seti.json" "$PROFIL"
cp "$STEND/lzhe_yadro.exe" "$YADRO_PAPKA/sing-box.exe"
# Уборка площадки. Сценарии 6-7 намеренно оставляют осиротевшее НАСТОЯЩЕЕ ядро
# в живых, а оно, умирая от SIGTERM, ещё успевает тронуть реестр — и его хвост
# приезжал в этот сценарий (замер 23.08: после сценария 8 в реестре стоял
# «http://127.0.0.1:2412» со схемой, а так пишет только настоящее ядро, наш
# Postavit пишет адрес без схемы). Здесь площадка чистится ЖЁСТКО и сверяется:
# лже-ядру никто не должен мешать, иначе сценарий судит не продукт, а соседа.
pkill -KILL -f '[K]elevra\.exe' 2>/dev/null
pkill -KILL -f '[s]ing-box\.exe' 2>/dev/null
sleep 2
if pgrep -f '[K]elevra\.exe|[s]ing-box\.exe' >/dev/null 2>&1; then
  echo "⚫ ПРИБОР МЁРТВ: на площадке сценария 8 остались чужие процессы:"
  pgrep -a -f '[K]elevra\.exe|[s]ing-box\.exe' | head -5
  exit 7
fi
reg_set 0 10.0.0.9:9999
proverka_do=$(reg_get ProxyServer)
if [ "$proverka_do" != "10.0.0.9:9999" ] || [ "$(reg_get ProxyEnable)" != "0x0" ]; then
  echo "⚫ ПРИБОР МЁРТВ: площадку не удалось выставить (ProxyEnable=$(reg_get ProxyEnable) ProxyServer=$proverka_do)"
  exit 7
fi
echo "  до: ProxyEnable=$(reg_get ProxyEnable) ProxyServer=$proverka_do (чужой выключенный прокси, метки нет, площадка чиста)"
url=$(zapustit_i_vzyat_url "$STEND/proksi_zapusk8.log")
if [ "$?" -eq 77 ]; then
  echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
  exit 7
fi
if [ -z "$url" ]; then
  echo "  служба не поднялась, журнал:"; tail -8 "$ZHURNAL" 2>/dev/null; bed=1
else
  echo "  служба: $url"
  echo "  перед подключением: ProxyEnable=$(reg_get ProxyEnable) ProxyServer=$(reg_get ProxyServer)"
  otvet=$(curl -s -m 120 "${url}api/podklyuchit")
  echo "  POST podklyuchit -> $otvet"
  sost=$(curl -s -m 10 "${url}api/sostoyanie")
  ruchnoy=$(printf '%s' "$sost" | ruchnoy_proksi_iz_sostoyaniya)
  sleep 2
  posle=$(reg_get ProxyEnable); server_posle=$(reg_get ProxyServer)
  echo "  после: ProxyEnable=$posle ProxyServer=$server_posle ruchnoy_proksi=$ruchnoy, метка: $([ -s "$METKA" ] && cat "$METKA" || echo 'НЕТ')"
  if printf '%s' "$otvet" | grep -q '"beda"'; then
    echo "  КРАСНЫЙ: подключение не удалось — тихий отказ проверить не на чем"
    tail -12 "$YADRO_PAPKA/yadro.log" 2>/dev/null; bed=1
  elif grep -q "system proxy" "$YADRO_PAPKA/yadro.log" 2>/dev/null; then
    echo "  КРАСНЫЙ: ядро всё-таки кричало про system proxy — сценарий свалился в старую громкую ветку, тихий отказ не воспроизведён"; bed=1
  elif [ "$posle" != "0x1" ]; then
    echo "  КРАСНЫЙ: ядро молча не поставило прокси, приложение это проглотило — ProxyEnable=$posle, а защита показана поднятой"; bed=1
  elif ! printf '%s' "$server_posle" | grep -qF "$ADRES_OZHIDAEMYY"; then
    echo "  КРАСНЫЙ: ProxyServer=$server_posle не содержит ожидаемый адрес $ADRES_OZHIDAEMYY — прописал не то"; bed=1
  elif [ "$ruchnoy" = "True" ]; then
    echo "  КРАСНЫЙ: реестр приложение прописало само правильно ($server_posle), а записка про ручную правку всё равно висит"; bed=1
  elif [ ! -s "$METKA" ]; then
    echo "  КРАСНЫЙ: прокси поставили мы, а метки нет — после жёсткой смерти снять его будет некому"; bed=1
  else
    echo "  зелёный: тихий отказ ядра пойман по реестру, прокси поставлен приложением ($server_posle), метка на месте"
  fi
fi
rm -f "$YADRO_PAPKA/tiho.marker"
ostanovit

echo "── итог: $([ $bed -eq 0 ] && echo ЗЕЛЁНЫЙ || echo КРАСНЫЙ) ──"
exit $bed
