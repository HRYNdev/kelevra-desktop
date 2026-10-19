#!/usr/bin/env bash
# Стенд «Автозапрос прав»: под 0.6.77 (e36411b) механизм, который этот стенд
# исходно проверял, МЁРТВ. До 0.6.77 ПЕРВОЕ подключение без прав само (фоном
# из podklyuchit) звало zaprositPravaAvtomaticheskiEsliNado и UAC. Проверено
# 19.10 трижды подряд grep'ом по internal/ и cmd/: этой функции, поля
# PravaUzheSprosheny-сеттера (OtmetitPravaZaprosheny) и строки «mozhno_tun» в
# JSON /api/sostoyanie — больше НЕТ НИГДЕ в продуктовом коде (только в
# устаревших комментариях hranenie.go:192,310,347, которые сами стали
# неправдой — владелец 17.10: «нет никаких неполных режимов»).
#
# Новый контракт (internal/sluzhba/sluzhba.go:PodnyatZashchitu, #2277): без
# прав — OshibkaNetPrav СРАЗУ, ядро не трогается, никакого фонового автозапроса
# на подключении. Новый автозапрос прав переехал на ХОЛОДНЫЙ СТАРТ приложения
# (cmd/kelevra/main.go: nuzhnoStavitSluzhbuNaStarte/postavitSluzhbuNaStarte —
# просит права на установку службы Windows, не на туннель) и явно выключен,
# когда tiho=true (nuzhnoStavitSluzhbuNaStarte: `!tiho && ...`). Этот стенд
# поднимает Kelevra.exe --tiho (чтобы не упереться в WebView2 под wine — см.
# okno_windows.go:134, «нет компонента WebView2, окно открыть нечем»,
# os.Exit(1) после блокирующего MessageBoxW), а значит холодный автозапрос
# прав этим харнесом СТРУКТУРНО не достать — см. шаг (ж) ниже, он честно
# остаётся ⚪, а не удалён молча.
#
# Диагноз красного прогона 19.10 (лог prava_avtozapros_DO_1019d.log): без
# KELEVRA_AVTOREZHIM_SHLYUZ контейнер физически в домашней сети владельца, honest
# domaSeychas видит настоящий домашний шлюз (MAC 80:af:ca:82:17:91) и
# «Подключиться» даже не доходит до PodnyatZashchitu («обстановка «дома» —
# защиту не поднимаю»): та же беда, что лечили в zhivoy_trafik.sh (81e34a3e)
# и zamer_skorosti.sh (11cb3c94) — см. их диффы. Лекарство то же.
#
# Каркас (сборка, WINEPREFIX, счёт живых копий, слежение за адресами) взят из
# stend/polnyy_rezhim.sh почти без изменений — там уже доказано, что ровно тот
# же механизм (sprositPrava -> prava.Poprosit -> ShellExecuteW verb=runas)
# под этим wine не рисует настоящее окно UAC, а сразу перезапускает бинарник.
#
# КРАСНЫЙ, если хоть один раз выполнено любое из:
#   (а) условие инверсии не подтвердилось ДО «Подключить»: /api/sostoyanie не
#       сказал prava=false, sluzhba_est=false, nuzhny_prava=true;
#   (б) «Подключить» без прав НЕ ответил 400 с беда=OshibkaNetPrav, ИЛИ
#       sost стал «rabotaet» (ядро поднялось без прав — контракт нарушен);
#   (в) в журнале хоть раз появилась строка старого мёртвого механизма
#       («при первом подключении» / «согласился на права администратора») —
#       это значило бы, что код воскрес, а стенд не заметил;
#   (г)/(д) — те же щупы, что в polnyy_rezhim.sh: больше одной живой unix-копии
#       Kelevra.exe дольше 2с подряд, или больше одного ОТВЕЧАЮЩЕГО адреса
#       службы одновременно, или в итоге живо не ровно 1 — отказ в правах не
#       имеет права тихо расплодить процессы;
#   (д2) после отказа копия вдруг показывает prava=true (самовольная эскалация
#       без UAC — хуже тишины);
#   (е) повторное «Подключить» дописывает в журнал строку старого механизма.
#
# Шаг (ж): холодный автозапрос прав на установку службы Windows (новый
# механизм) не воспроизведён — требует реальной Windows SCM-установки под
# wine (icacls есть, SCM не проверен) и запуска БЕЗ --tiho, что бьётся в
# WebView2. Отдельный наряд, не этот.
#
# Чего стенд НЕ проверяет: постановку системного прокси (её больше нет вовсе,
# 0.6.77) и отмену UAC человеком (под wine рычага заставить ShellExecuteW
# отказать нет — та же оговорка в шапке polnyy_rezhim.sh).
set -u

# schitat_kopii — сколько копий Kelevra.exe живо ПРЯМО СЕЙЧАС. Дословно взято
# из stend/polnyy_rezhim.sh (там же разбор двух граблей — `pgrep -c` при
# нуле совпадений и матч по `-f` вместо `-x`), чтобы не наступить на те же.
schitat_kopii() {
  local n
  n=$(pgrep -c -x "Kelevra.exe" 2>/dev/null)
  [ -n "$n" ] || n=0
  printf '%s' "$n"
}

KORFN=$(cd "$(dirname "$0")/.." && pwd)
WINE=${WINE:-/usr/lib/wine/wine64}
export WINEPREFIX=${WINEPREFIX:-$KORFN/.wine}
export WINEDEBUG=${WINEDEBUG:--all}
export HOME=${HOME:-/root}
export LANG=${LANG:-C.UTF-8} LC_ALL=${LC_ALL:-C.UTF-8}
STEND=$KORFN/.stend_prava_avto
mkdir -p "$STEND" "$WINEPREFIX"
. "$KORFN/stend/obshchee.sh"

# AVTOREZHIM_DNS_PODMENA — см. пояснение в stend/proksi.sh: этот контейнер
# сам отвечает fake-ip подменой на контрольные домены, поэтому domaSeychas
# (podklyuchit, #78) без подмены честно, но ложно решает «дома» и защиту не
# поднимает. 127.0.0.1:1 никто не слушает — мгновенный ECONNREFUSED, зонд
# честно решает «не дома».
AVTOREZHIM_DNS_PODMENA="127.0.0.1:1"
# AVTOREZHIM_SHLYUZ_PODMENA — см. пояснение в zhivoy_trafik.sh (81e34a3e):
# без ОБЕИХ подмен domaSeychas видит настоящий домашний шлюз контейнера и
# честно, но ложно решает «дома» — «Подключиться» тогда даже не доходит до
# PodnyatZashchitu (замерено 19.10: prava_avtozapros_DO_1019d.log, «обстановка
# «дома» — защиту не поднимаю»), и шаг (б) ниже нечего проверять.
AVTOREZHIM_SHLYUZ_PODMENA="de:ad:be:ef:00:01"

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
YADRO_PAPKA="$PAPKA/yadro"
ZHURNAL="$PAPKA/kelevra.log"
METKA="$PAPKA/zapushcheno.json"
PROFIL="$PAPKA/profil.json"
NASTROYKI="$PAPKA/nastroyki.json"
# Настоящее ядро для Windows — общий кеш стенда proksi.sh, а не своя копия:
# это тот же файл, что качает/держит stend/windows.sh, тратить лишние
# десятки МБ ради изоляции незачем.
YADRO_ISTOCHNIK="$KORFN/.stend_win/sing-box.exe"
bed=0

echo "── сборка Kelevra.exe (windows/amd64) ──"
if ! GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui" -o "$STEND/Kelevra.exe" "$KORFN/cmd/kelevra" 2>&1; then
  echo "  НЕ СОБРАЛСЯ"; exit 1
fi

if [ ! -s "$YADRO_ISTOCHNIK" ]; then
  echo "⚫ ПРИБОР МЁРТВ: нет настоящего ядра ($YADRO_ISTOCHNIK, качает stend/windows.sh) — сценарий нечем поднимать"
  exit 7
fi

echo "── площадка: свежий инсталл (нет nastroyki.json — PravaZaprosheny=false), профиль с туннелем, без прав ──"
pkill -f "Kelevra.exe" 2>/dev/null; sleep 1
mkdir -p "$YADRO_PAPKA"
rm -f "$ZHURNAL" "$METKA" "$NASTROYKI"
# profil_stend_bez_seti.json — тот же профиль-близнец, что в сценариях 4/5/8
# stend/proksi.sh: вход tun (даёт EstTunnel=true) + вход mixed (даёт
# Proksi-режим, пока прав нет), без сетевых rule_set (не падает FATAL без
# интернета у стенда).
cp "$KORFN/internal/konfig/testdata/profil_stend_bez_seti.json" "$PROFIL"
cp "$YADRO_ISTOCHNIK" "$YADRO_PAPKA/sing-box.exe"

echo "── поднимаю --tiho (служба живёт отдельным процессом, окна нет — под wine WebView2 недоступен) ──"
wine_zapusti "$STEND/start.log" "$ZHURNAL" "служба слушает" 20 -- \
  env KELEVRA_BEZ_OBNOVLENIYA=1 KELEVRA_AVTOREZHIM_DNS="$AVTOREZHIM_DNS_PODMENA" \
  KELEVRA_AVTOREZHIM_SHLYUZ="$AVTOREZHIM_SHLYUZ_PODMENA" \
  timeout 60 "$WINE" "$STEND/Kelevra.exe" --tiho
mertv=$?
if [ "$mertv" -eq 77 ]; then
  echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
  exit 7
fi

if [ ! -f "$METKA" ]; then
  echo "  КРАСНЫЙ окружения: служба не отметилась меткой — сценарию не с чего начинать"
  pkill -f "Kelevra.exe" 2>/dev/null
  exit 1
fi
staryy_adres=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))['url'])" "$METKA" 2>/dev/null)
echo "  служба поднялась, адрес: $staryy_adres"

bazovyy=$(schitat_kopii)
echo "  процессов Kelevra.exe до подключения: $bazovyy"
if [ "$bazovyy" -ne 1 ]; then
  echo "  КРАСНЫЙ окружения: ожидали ровно 1 процесс до старта сценария, живо $bazovyy — площадка нечистая"
  pkill -f "Kelevra.exe" 2>/dev/null
  exit 1
fi

echo "── (а) проверяю условие инверсии ДО «Подключить»: /api/sostoyanie ──"
sost_do=$(curl -s -m 10 "${staryy_adres}api/sostoyanie")
echo "  $sost_do"
usloviye=$(printf '%s' "$sost_do" | python3 -c '
import json,sys
try:
    d=json.load(sys.stdin)
except Exception:
    print("False"); raise SystemExit
print((not d.get("prava", True)) and (not d.get("sluzhba_est", True)) and bool(d.get("nuzhny_prava", False)))
')
if [ "$usloviye" != "True" ]; then
  echo "  (а) КРАСНЫЙ: условие (prava=false, sluzhba_est=false, nuzhny_prava=true) не подтвердилось — сценарию нечего доказывать"; bed=1
else
  echo "  (а) зелёный: прав и службы нет, продукт сам знает, что они нужны — есть с чего начинать"
fi

echo "── (б) человек нажимает «Подключить»: POST ${staryy_adres}api/podklyuchit ──"
otvet=$(curl -s -w '\n%{http_code}' -m 60 -X POST "${staryy_adres}api/podklyuchit")
kod=$(printf '%s' "$otvet" | tail -1)
telo=$(printf '%s' "$otvet" | sed '$d')
echo "  http_code=$kod, тело: $telo"
if ploshchadka_ne_tyanet "$ZHURNAL"; then
  echo "ПЛОЩАДКА: wine не отдаёт список сетевых адаптеров, вердикт по продукту не выношу"
  pkill -f "Kelevra.exe" 2>/dev/null
  exit 7
fi
# Инверсия (PodnyatZashchitu, sluzhba.go:2277): без прав — 400 с OshibkaNetPrav
# СРАЗУ, никакого фонового автозапроса больше нет (см. шапку).
if [ "$kod" != "400" ] || ! printf '%s' "$telo" | grep -qF 'Для работы Kelevra нужны права администратора.'; then
  echo "  (б) КРАСНЫЙ: «Подключить» без прав не ответил 400+OshibkaNetPrav (код $kod, тело $telo)"
  bed=1
else
  echo "  (б) зелёный: отказ сразу, беда=«Для работы Kelevra нужны права администратора.»"
fi
sost1=$(curl -s -m 10 "${staryy_adres}api/sostoyanie")
sost1_val=$(printf '%s' "$sost1" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("sost","?"))' 2>/dev/null)
if [ "$sost1_val" = "rabotaet" ]; then
  echo "  (б) КРАСНЫЙ: sost=rabotaet — ядро поднялось без прав, контракт 0.6.77 нарушен"
  bed=1
else
  echo "  (б) зелёный: sost=$sost1_val (не rabotaet) — ядро без прав не трогалось"
fi

echo "── (в) журнал не должен содержать строк МЁРТВОГО механизма (жду 3с на всякий случай) ──"
sleep 3
if grep -qE "при первом подключении" "$ZHURNAL" 2>/dev/null; then
  echo "  (в) КРАСНЫЙ: в журнале воскресла строка старого механизма — «$(grep -E 'при первом подключении' "$ZHURNAL")»"
  bed=1
else
  echo "  (в) зелёный: журнал не знает про «при первом подключении» — старый механизм остался мёртв"
fi

echo "── (г)/(д) слежу за процессами и адресами 10 раз/сек, 5 секунд: отказ в правах не должен тихо расплодить процессы ──"
# Смены копии здесь больше НЕТ (б) отвечает 400 и возвращается немедленно,
# никто никого не повышает — порог streak_dvuh оставлен тем же щупом, что в
# polnyy_rezhim.sh, просто как регрессия «инверсия не плодит лишние unix-копии».
ryad=""
max_procs=1
max_zhivyh_adresov=1
streak_dvuh=0
max_streak_dvuh=0
for _ in $(seq 1 50); do
  cnt=$(schitat_kopii)
  ryad="$ryad $cnt"
  [ "$cnt" -gt "$max_procs" ] && max_procs=$cnt
  if [ "$cnt" -gt 1 ]; then
    streak_dvuh=$((streak_dvuh + 1))
    [ "$streak_dvuh" -gt "$max_streak_dvuh" ] && max_streak_dvuh=$streak_dvuh
  else
    streak_dvuh=0
  fi

  zhivyh=0
  for a in $(grep -oE 'служба слушает http://[^[:space:]]+' "$ZHURNAL" 2>/dev/null | awk '{print $3}' | sort -u); do
    kod_a=$(curl -s -m 0.3 -o /dev/null -w '%{http_code}' "${a}api/sostoyanie" 2>/dev/null)
    case "$kod_a" in 2??) zhivyh=$((zhivyh + 1)) ;; esac
  done
  [ "$zhivyh" -gt "$max_zhivyh_adresov" ] && max_zhivyh_adresov=$zhivyh

  sleep 0.1
done
echo "  ряд процессов:$ryad"
echo "  максимум одновременно живых unix-процессов Kelevra.exe: $max_procs (подряд замеров с >1: $max_streak_dvuh из 50, по 100мс каждый)"
echo "  максимум одновременно ОТВЕЧАЮЩИХ адресов службы: $max_zhivyh_adresov"

final=$(schitat_kopii)
echo "  процессов Kelevra.exe после settle: $final"

porog_streaka=20
if [ "$max_streak_dvuh" -gt "$porog_streaka" ]; then
  echo "  (г) КРАСНЫЙ: $max_streak_dvuh замеров подряд (>$((porog_streaka / 10))с) видели больше одного процесса Kelevra.exe — старая копия зависла, новая не дождалась"
  bed=1
else
  echo "  (г) зелёный: живо больше одного процесса Kelevra.exe было не дольше $((porog_streaka / 10))с подряд (макс. $max_procs, $max_streak_dvuh замеров подряд)"
fi

if [ "$max_zhivyh_adresov" -gt 1 ]; then
  echo "  (г) КРАСНЫЙ: одновременно отвечали $max_zhivyh_adresov разных адреса службы — два экземпляра работали бок о бок"
  bed=1
else
  echo "  (г) зелёный: ни разу не отвечало больше одного адреса службы одновременно"
fi

if [ "$final" -ne 1 ]; then
  echo "  (д) КРАСНЫЙ: живых копий Kelevra.exe в итоге $final (беда 25.08 с двумя окнами — ждали ровно 1)"
  bed=1
else
  echo "  (д) зелёный: живых копий Kelevra.exe в итоге ровно 1"
fi

echo "── (д2) та же копия НЕ получила права сама собой (нет тихой эскалации) ──"
noviy_adres=$(grep -oE 'служба слушает http://[^[:space:]]+' "$ZHURNAL" 2>/dev/null | awk '{print $3}' | tail -1)
echo "  адрес из журнала: $noviy_adres"
if [ -z "$noviy_adres" ]; then
  echo "  (д2) КРАСНЫЙ: в журнале нет ни одного адреса службы"
  bed=1
else
  sost_posle=$(curl -s -m 10 "${noviy_adres}api/sostoyanie")
  echo "  /api/sostoyanie -> $sost_posle"
  bez_eskalatsii=$(printf '%s' "$sost_posle" | python3 -c '
import json,sys
try:
    d=json.load(sys.stdin)
except Exception:
    print("False"); raise SystemExit
print((not d.get("prava", True)) and (not d.get("sluzhba_est", True)))
')
  if [ "$bez_eskalatsii" != "True" ]; then
    echo "  (д2) КРАСНЫЙ: копия вдруг показывает prava=true/sluzhba_est=true — самовольная эскалация без UAC"
    bed=1
  else
    echo "  (д2) зелёный: prava=false, sluzhba_est=false — отказ остался отказом, никто не самовольничал"
  fi
fi

echo "── (е) повторное «Подключить» тоже не воскрешает мёртвый механизм ──"
do_povtora=$(grep -c "при первом подключении" "$ZHURNAL" 2>/dev/null)
if [ -n "$noviy_adres" ]; then
  otvet2=$(curl -s -m 60 -X POST "${noviy_adres}api/podklyuchit")
  sleep 2
  posle_povtora=$(grep -c "при первом подключении" "$ZHURNAL" 2>/dev/null)
  echo "  повторный POST podklyuchit -> $otvet2"
  echo "  строк «при первом подключении» в журнале: было $do_povtora, стало $posle_povtora"
  if [ "$posle_povtora" -ne "$do_povtora" ]; then
    echo "  (е) КРАСНЫЙ: повторное подключение дописало строку мёртвого механизма — он воскрес"
    bed=1
  else
    echo "  (е) зелёный: повторное подключение журнал не тронуло"
  fi
else
  echo "  (е) КРАСНЫЙ: нечем проверить — нет адреса выжившей копии"
  bed=1
fi

echo "── (ж) холодный автозапрос прав на установку службы Windows (новый механизм, cmd/kelevra/main.go: nuzhnoStavitSluzhbuNaStarte) ──"
if [ "${KELEVRA_STEND_SCM:-0}" = "1" ]; then
  echo "  (ж) КРАСНЫЙ: KELEVRA_STEND_SCM=1 поставлен, но ветка с реальной установкой службы Windows под wine (SCM, icacls) в этом наряде не реализована — живой прогон этой веткой нужен ОТДЕЛЬНЫМ стендом, не этим"
  bed=1
else
  echo "  (ж) ⚪ не проверяется: харнесс поднимает Kelevra.exe --tiho (чтобы не упереться в WebView2 под wine, окно нечем рисовать — okno_windows.go:134), а nuzhnoStavitSluzhbuNaStarte явно выключает автозапрос при tiho=true. Проверка реальной установки службы Windows через SCM — отдельный наряд."
fi

echo "── журнал (хвост) ──"
tail -30 "$ZHURNAL" 2>/dev/null | sed 's/^/  /'

pkill -f "Kelevra.exe" 2>/dev/null

echo "── итог: $([ $bed -eq 0 ] && echo ЗЕЛЁНЫЙ || echo КРАСНЫЙ) ──"
exit $bed
