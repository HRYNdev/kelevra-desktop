#!/usr/bin/env bash
# Стенд «Автозапрос прав»: доказывает живьём (настоящая windows-сборка под
# wine, настоящее ядро sing-box.exe), что ПЕРВОЕ успешное подключение с
# профилем, которому нужен туннель (EstTunnel=true), но без прав
# администратора, само — без единого клика человека сверх «Подключить» —
# спрашивает права и доводит смену копии до конца ровно одним живым
# процессом (internal/sluzhba/sluzhba.go: zaprositPravaAvtomaticheskiEsliNado,
# вызывается фоном из podklyuchit).
#
# Почему отдельно от stend/proksi.sh (сценарии 4/5/8: тот же EstTunnel=true,
# права=false, настоящее ядро). Эти сценарии зовут wine_zapusti с taimaut=20 и
# затем сами гасят процесс сразу после ответа /api/podklyuchit (ostanovit) —
# горутина zaprositPravaAvtomaticheskiEsliNado просто не успевает дойти до
# ShellExecuteW. Замерено 27.08: в .stend_win/proksi_zapusk4.log нет ни одной
# строки «при первом подключении». Этот стенд НЕ гасит процесс после ответа —
# он ждёт автозапрос и смену копии так же, как stend/polnyy_rezhim.sh ждёт
# смену по кнопке «Полная защита».
#
# Каркас (сборка, WINEPREFIX, счёт живых копий, слежение за адресами) взят из
# stend/polnyy_rezhim.sh почти без изменений — там уже доказано, что ровно тот
# же механизм (sprositPrava -> prava.Poprosit -> ShellExecuteW verb=runas)
# под этим wine не рисует настоящее окно UAC, а сразу перезапускает бинарник
# так, что новая копия видит права=true (тот стенд зелёный на кнопке «Полная
# защита»). Здесь тот же механизм зовёт не кнопка, а сам продукт после
# первого подключения — разница только в ТРИГГЕРЕ и в ожидаемых строках
# журнала.
#
# КРАСНЫЙ, если хоть один раз выполнено любое из:
#   (а) условие автозапроса не подтвердилось ДО нажатия «Подключить»:
#       /api/sostoyanie не сказал mozhno_tun=true, prava=false;
#   (б) «Подключить» не ответил gotovo=true, ИЛИ подключение (реестр
#       прокси проверяет stend/proksi.sh, тут проверяем только сам факт
#       успеха) не состоялось раньше, чем в журнале появилась строка
#       автозапроса — то есть если бы автозапрос когда-нибудь стал
#       синхронным и придержал ответ, это должно быть видно по времени;
#   (в) за разумный срок после ответа в журнале НЕ появилась строка
#       автозапроса прав («…при первом подключении…»), либо появилась
#       строка отказа вместо согласия (под этим wine ShellExecuteW уже
#       доказанно соглашается сам — см. stend/polnyy_rezhim.sh);
#   (г)/(д) — те же два щупа, что в polnyy_rezhim.sh: больше одной живой
#       unix-копии Kelevra.exe дольше 2с подряд, или больше одного
#       ОТВЕЧАЮЩЕГО адреса службы одновременно, или в итоге живо не ровно 1;
#   (д2) новая копия после смены не видит prava=true и rezhim=tunnel;
#   (е) повторное «Подключить» на выжившей копии дописывает в журнал ЕЩЁ ОДНУ
#       строку автозапроса — флаг PravaZaprosheny должен запирать вопрос
#       навсегда после первого раза.
#
# Чего стенд НЕ проверяет: постановку системного прокси в реестре в режиме
# tunnel (для этого нужен отдельный разбор, см. stend/proksi.sh) и отмену
# UAC человеком (под wine рычага заставить ShellExecuteW отказать нет — см.
# ту же оговорку в шапке polnyy_rezhim.sh; отказ покрыт go-тестом
# internal/sluzhba/prava_avto_test.go).
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

echo "── (а) проверяю условие автозапроса ДО «Подключить»: /api/sostoyanie ──"
sost_do=$(curl -s -m 10 "${staryy_adres}api/sostoyanie")
echo "  $sost_do"
usloviye=$(printf '%s' "$sost_do" | python3 -c '
import json,sys
try:
    d=json.load(sys.stdin)
except Exception:
    print("False"); raise SystemExit
print(bool(d.get("mozhno_tun", False)) and not bool(d.get("prava", True)))
')
if [ "$usloviye" != "True" ]; then
  echo "  (а) КРАСНЫЙ: условие автозапроса (mozhno_tun=true, prava=false) не подтвердилось — сценарию нечего доказывать"; bed=1
else
  echo "  (а) зелёный: mozhno_tun=true и prava=false — есть с чего начинать"
fi

echo "── (б) человек нажимает «Подключить»: POST ${staryy_adres}api/podklyuchit ──"
do_zaprosa=$(date +%s.%N)
otvet=$(curl -s -w '\n%{http_code}' -m 60 -X POST "${staryy_adres}api/podklyuchit")
posle_otveta=$(date +%s.%N)
kod=$(printf '%s' "$otvet" | tail -1)
telo=$(printf '%s' "$otvet" | sed '$d')
dlitelnost=$(python3 -c "print(f'{$posle_otveta - $do_zaprosa:.2f}')" 2>/dev/null || echo "?")
echo "  http_code=$kod, тело: $telo, ответ за ${dlitelnost}с"
if [ "$kod" != "200" ] || ! printf '%s' "$telo" | grep -q '"gotovo":true' || printf '%s' "$telo" | grep -q '"beda"'; then
  echo "  (б) КРАСНЫЙ: «Подключить» не ответил честным gotovo=true (код $kod, тело $telo)"
  bed=1
else
  echo "  (б) зелёный: подключение состоялось (gotovo=true), запрос прав в ответе не отражён — он идёт фоном"
fi
# Строку автозапроса ждём НИЖЕ отдельным циклом; тут фиксируем только то,
# что curl уже получил ответ, а не завис внутри него — ответ обязан прийти
# заметно быстрее, чем идёт вся цепочка ShellExecuteW+смена копии (десятки
# секунд ниже), иначе «не дожидаясь прав» — неправда.
if [ "$kod" = "200" ] && python3 -c "exit(0 if float('$dlitelnost') < 5 else 1)" 2>/dev/null; then
  :
elif [ "$kod" = "200" ]; then
  echo "  (б) КРАСНЫЙ: ответ шёл ${dlitelnost}с — подозрительно долго для «не дожидаясь прав»"; bed=1
fi

echo "── (в) жду строку автозапроса прав в журнале (до 25с) ──"
soglasilsya=0
otkaz=0
for _ in $(seq 1 250); do
  if grep -q "человек согласился на права администратора при первом подключении" "$ZHURNAL" 2>/dev/null; then
    soglasilsya=1; break
  fi
  if grep -q "автозапрос прав администратора при первом подключении" "$ZHURNAL" 2>/dev/null; then
    otkaz=1; break
  fi
  sleep 0.1
done
if [ "$soglasilsya" -eq 1 ]; then
  echo "  (в) зелёный: журнал — «$(grep 'человек согласился на права администратора при первом подключении' "$ZHURNAL")»"
elif [ "$otkaz" -eq 1 ]; then
  echo "  (в) КРАСНЫЙ: автозапрос случился, но кончился отказом/ошибкой вместо согласия — «$(grep 'автозапрос прав администратора при первом подключении' "$ZHURNAL")»"
  bed=1
else
  echo "  (в) КРАСНЫЙ: за 25с в журнале не появилось ни строки согласия, ни строки отказа — автозапрос вообще не выполнился"
  bed=1
fi

echo "── (г)/(д) слежу за процессами и адресами 10 раз/сек, 15 секунд (смена копии) ──"
# Порог и смысл streak_dvuh — дословно как в stend/polnyy_rezhim.sh: новая,
# уже повышенная копия сама ЖДЁТ смерти старой (zhdatSmenu, cmd/kelevra/
# main.go), и короткое перекрытие на эти доли секунды — не беда сама по
# себе, беда — зависание за пределами потолка ожидания (10с, см.
# srokOzhidaniyaSmeny).
ryad=""
max_procs=1
max_zhivyh_adresov=1
streak_dvuh=0
max_streak_dvuh=0
for _ in $(seq 1 150); do
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
echo "  максимум одновременно живых unix-процессов Kelevra.exe: $max_procs (подряд замеров с >1: $max_streak_dvuh из 150, по 100мс каждый)"
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
  echo "  (д) КРАСНЫЙ: живых копий Kelevra.exe в итоге $final (беда 25.08 «2 нахуй открыто» — ждали ровно 1)"
  bed=1
else
  echo "  (д) зелёный: живых копий Kelevra.exe в итоге ровно 1"
fi

echo "── (д2) новая копия видит права=true и режим=tunnel ──"
noviy_adres=$(grep -oE 'служба слушает http://[^[:space:]]+' "$ZHURNAL" 2>/dev/null | awk '{print $3}' | tail -1)
echo "  последний адрес из журнала: $noviy_adres"
if [ -z "$noviy_adres" ]; then
  echo "  (д2) КРАСНЫЙ: в журнале нет ни одного адреса службы"
  bed=1
else
  sost_posle=$(curl -s -m 10 "${noviy_adres}api/sostoyanie")
  echo "  /api/sostoyanie -> $sost_posle"
  provereno=$(printf '%s' "$sost_posle" | python3 -c '
import json,sys
try:
    d=json.load(sys.stdin)
except Exception:
    print("False"); raise SystemExit
print(bool(d.get("prava", False)) and d.get("rezhim")=="tunnel" and bool(d.get("prava_uzhe_sprosheny", False)))
')
  if [ "$provereno" != "True" ]; then
    echo "  (д2) КРАСНЫЙ: новая копия не показывает prava=true/rezhim=tunnel/prava_uzhe_sprosheny=true"
    bed=1
  else
    echo "  (д2) зелёный: новая копия — prava=true, rezhim=tunnel, prava_uzhe_sprosheny=true"
  fi
fi

echo "── (е) повторное «Подключить» больше прав не просит ──"
do_povtora=$(grep -c "при первом подключении" "$ZHURNAL" 2>/dev/null)
if [ -n "$noviy_adres" ]; then
  otvet2=$(curl -s -m 60 -X POST "${noviy_adres}api/podklyuchit")
  sleep 2
  posle_povtora=$(grep -c "при первом подключении" "$ZHURNAL" 2>/dev/null)
  echo "  повторный POST podklyuchit -> $otvet2"
  echo "  строк «при первом подключении» в журнале: было $do_povtora, стало $posle_povtora"
  if [ "$posle_povtora" -ne "$do_povtora" ]; then
    echo "  (е) КРАСНЫЙ: повторное подключение дописало ещё одну строку автозапроса — флаг PravaZaprosheny не запер вопрос"
    bed=1
  else
    echo "  (е) зелёный: повторное подключение прав не спросило снова"
  fi
else
  echo "  (е) КРАСНЫЙ: нечем проверить — нет адреса выжившей копии"
  bed=1
fi

echo "── журнал (хвост) ──"
tail -30 "$ZHURNAL" 2>/dev/null | sed 's/^/  /'

pkill -f "Kelevra.exe" 2>/dev/null

echo "── итог: $([ $bed -eq 0 ] && echo ЗЕЛЁНЫЙ || echo КРАСНЫЙ) ──"
exit $bed
