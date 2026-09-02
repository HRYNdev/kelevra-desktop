#!/usr/bin/env bash
# Стенд «Полная защита» (UAC): доказывает живьём (настоящая windows-сборка
# под wine), что переключение в полный режим не поднимает вторую пару
# окно+служба, пока жива первая.
#
# Жалоба 25.08: при запуске и включении полного режима снова открывалось
# два окна.
#
# Диагноз (internal/sluzhba/sluzhba.go: polnayaZashchita, internal/prava/
# prava_windows.go: Poprosit): метка единственного экземпляра снимается ДО
# окна UAC, ShellExecuteW запускает повышенную копию БЕЗ аргументов и без
# всякой синхронизации со старой, а старая гасит себя фиксированным
# time.Sleep(300ms) и os.Exit — без гарантии, что успела выйти. Между «метка
# снята» и «старая копия реально умерла» проходит время, в которое
# повышенная копия стартует как ПЕРВАЯ и поднимает своё окно/трей/службу
# поверх ещё живой старой.
#
# Что стенд ДЕЛАЕТ: поднимает Kelevra.exe --tiho (служба уже живёт, окна нет
# — под wine WebView2 всё равно недоступен, см. razdvoenie.sh), шлёт ей
# POST /api/polnaya_zashchita (то самое «включил полный режим») и следит за
# машиной 10 раз в секунду несколько секунд подряд.
#
# Починка (см. диагноз выше): метка теперь живёт у старой копии до её
# смерти, а не снимается заранее; ShellExecuteW передаёт новой копии pid
# старой аргументом --smena, и та копия сама ждёт (zhdatSmenu,
# cmd/kelevra/main.go) подтверждённой смерти старой — вместо гонки на
# фиксированном time.Sleep. Пока новая ждёт, она не тихая копия
# «уже работает», а лишь unix-процесс без своего адреса и без своего трея —
# короткое перекрытие с умирающей старой на этой стадии ожидаемо и не беда.
#
# КРАСНЫЙ, если хоть один раз выполнено любое из:
#   (a) больше одного unix-процесса "Kelevra.exe" живо дольше 2 секунд
#       ПОДРЯД — не разовый всплеск (ожидание смерти старой копии само по
#       себе создаёт короткое перекрытие, это не беда), а зависание за
#       пределами потолка ожидания новой копии (10с, см. srokOzhidaniyaSmeny);
#   (b) СРАЗУ ДВА разных адреса службы (kelevra.log: «служба слушает …»)
#       одновременно отвечают по HTTP — не просто оба когда-либо
#       засветились в журнале (смена адреса при переключении режима сама по
#       себе ожидаема — новый экземпляр слушает новый случайный порт), а
#       именно то, что старый и новый ОБА живы в один и тот же момент;
#   (d) через разумный срок после ответа службы не осталось РОВНО одного
#       живого процесса.
# Отдельно, только как подсказка для человека (не отдельный gate — HWND
# трея меняется и в исправленном мире, это не бага сама по себе), стенд
# печатает, сколько разных «hwnd=» засветилось в журнале.
#
# Чего стенд НЕ ПРОВЕРЯЕТ, честно: отмену UAC (человек нажал «Нет» — метку
# никто не трогал, старая копия остаётся жить как ни в чём не бывало). Под
# wine нет рычага, которым можно заставить настоящий ShellExecuteW отказать
# — это не флаг стенда, а собственно то, о чём спрашивает человека Windows.
# Этот случай покрыт go-тестами internal/sluzhba/polnaya_zashchita_test.go
# (TestOtkazVPravahOstavlyaetMetkuNaMeste), которые гоняются в приёмке отдельно.
set -u

# schitat_kopii — сколько копий Kelevra.exe живо ПРЯМО СЕЙЧАС.
#
# Две грабли, из-за которых прежний однострочник врал молча (27.08):
#  1) `pgrep -c` при нуле совпадений ПЕЧАТАЕТ «0» и возвращает rc=1 — то есть
#     хвост `|| echo 0` дописывал ВТОРОЙ ноль, и переменная получала «0\n0».
#     Дальше `[ "$final" -ne 1 ]` падал с «integer expression expected», а
#     падение условия уводит в ветку else — щуп (d) печатал ЗЕЛЁНЫЙ на пустой
#     площадке. Зелёный поверх пустоты; за весь срок жизни щуп не сработал ни разу.
#  2) `-f` матчит ВСЮ командную строку, а значит и любую соседнюю оболочку, в
#     чьей строке просто встретилось это имя (свой же grep, свой же pgrep, вызов
#     стенда из скрипта). Считаем по ИМЕНИ процесса (-x, без -f): под wine оно
#     ровно «Kelevra.exe» и в 15 символов /proc/comm влезает целиком.
schitat_kopii() {
  local n
  n=$(pgrep -c -x "Kelevra.exe" 2>/dev/null)
  # rc=1 (никого не нашёл) — не ошибка, это честный ноль.
  [ -n "$n" ] || n=0
  printf '%s' "$n"
}

KORFN=$(cd "$(dirname "$0")/.." && pwd)
WINE=${WINE:-/usr/lib/wine/wine64}
export WINEPREFIX=${WINEPREFIX:-$KORFN/.wine}
export WINEDEBUG=${WINEDEBUG:--all}
export HOME=${HOME:-/root}
export LANG=${LANG:-C.UTF-8} LC_ALL=${LC_ALL:-C.UTF-8}
STEND=$KORFN/.stend_rezhim
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

pkill -f "Kelevra.exe" 2>/dev/null; sleep 1
rm -f "$ZHURNAL" "$METKA"

echo "── поднимаю --tiho (служба живёт, окна нет — под wine WebView2 недоступен) ──"
wine_zapusti "$STEND/start.log" "$ZHURNAL" "служба слушает" 20 -- \
  env KELEVRA_BEZ_OBNOVLENIYA=1 timeout 60 "$WINE" "$STEND/Kelevra.exe" --tiho
mertv=$?
if [ "$mertv" -eq 77 ]; then
  echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
  exit 7
fi

if [ ! -f "$METKA" ]; then
  echo "  КРАСНЫЙ окружения: служба не отметилась меткой — сценарий не с чего начинать"
  pkill -f "Kelevra.exe" 2>/dev/null
  exit 1
fi
staryy_adres=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))['url'])" "$METKA" 2>/dev/null)
echo "  служба поднялась, адрес: $staryy_adres"

bazovyy=$(schitat_kopii)
echo "  процессов Kelevra.exe до переключения режима: $bazovyy"
if [ "$bazovyy" -ne 1 ]; then
  echo "  КРАСНЫЙ окружения: ожидали ровно 1 процесс до старта сценария, живо $bazovyy — площадка нечистая"
  pkill -f "Kelevra.exe" 2>/dev/null
  exit 1
fi

echo "── человек нажимает «Полная защита»: POST ${staryy_adres}api/polnaya_zashchita ──"
otvet=$(curl -s -w '\n%{http_code}' -X POST "${staryy_adres}api/polnaya_zashchita")
kod=$(printf '%s' "$otvet" | tail -1)
telo=$(printf '%s' "$otvet" | sed '$d')
echo "  http_code=$kod, тело: $telo"
if [ "$kod" != "200" ]; then
  echo "  КРАСНЫЙ: запрос «Полная защита» не удался (код $kod) — сценарий смены режима не сработал вовсе"
  pkill -f "Kelevra.exe" 2>/dev/null
  exit 1
fi

echo "── слежу за процессами и адресами 10 раз/сек, 15 секунд ──"
ryad=""
max_procs=1
max_zhivyh_adresov=1
# streak_dvuh — сколько ПОДРЯД замеров (по 100мс) видели больше одного
# unix-процесса Kelevra.exe. Отличаем от разового «>1»: новая, уже
# повышенная копия сама ЖДЁТ смерти старой (zhdatSmenu, cmd/kelevra/
# main.go), и на эти доли секунды, пока старая дожидается своего
# time.Sleep(300ms) и выходит, новая уже существует как unix-процесс, но
# ещё не поднимает ни своего адреса, ни своего трея, — это не беда, а сама
# суть безопасной передачи смены. Бедой это становится, только если
# зависает: старая копия не умерла, а новая всё равно решила не ждать
# (потолок zhdatSmenu — 10 секунд, см. srokOzhidaniyaSmeny) — тогда подряд
# идущих замеров с >1 наберётся на секунды, а не на первые доли секунды
# после ответа.
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

  # (b) сколько РАЗНЫХ адресов из журнала отвечают по HTTP ПРЯМО СЕЙЧАС —
  # не «когда-либо засветились», а живы одновременно.
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

adresov_v_zhurnale=$(grep -oE 'служба слушает http://[^[:space:]]+' "$ZHURNAL" 2>/dev/null | awk '{print $3}' | sort -u | wc -l)
hwnd_v_zhurnale=$(grep -oE 'hwnd=0x[0-9a-f]+' "$ZHURNAL" 2>/dev/null | sort -u | wc -l)
echo "  подсказка: разных адресов за весь прогон в журнале — $adresov_v_zhurnale (смена адреса при переключении режима ожидаема сама по себе)"
echo "  подсказка: разных hwnd трея за весь прогон в журнале — $hwnd_v_zhurnale (новое окно трея при переключении режима ожидаемо само по себе)"

final=$(schitat_kopii)
echo "  процессов Kelevra.exe после settle: $final"

# Порог в 20 замеров (2с) отделяет ожидаемое короткое перекрытие «старая
# дожидается своего time.Sleep(300ms), новая уже существует, но молча ждёт
# её смерти» от настоящего зависания (потолок ожидания у новой копии —
# 10 секунд, см. srokOzhidaniyaSmeny в cmd/kelevra/main.go).
porog_streaka=20
if [ "$max_streak_dvuh" -gt "$porog_streaka" ]; then
  echo "  (a) КРАСНЫЙ: $max_streak_dvuh замеров подряд (>$((porog_streaka / 10))с) видели больше одного процесса Kelevra.exe — старая копия зависла, а новая не дождалась и всё равно поднялась"
  bed=1
else
  echo "  (a) зелёный: живо больше одного процесса Kelevra.exe было не дольше $((porog_streaka / 10))с подряд (макс. $max_procs процесса, $max_streak_dvuh замеров подряд) — это ожидание смерти старой копии, а не гонка"
fi

if [ "$max_zhivyh_adresov" -gt 1 ]; then
  echo "  (b) КРАСНЫЙ: одновременно отвечали $max_zhivyh_adresov разных адреса службы — два экземпляра работали бок о бок"
  bed=1
else
  echo "  (b) зелёный: ни разу не отвечало больше одного адреса службы одновременно"
fi

if [ "$final" -ne 1 ]; then
  echo "  (d) КРАСНЫЙ: после переключения режима живо процессов Kelevra.exe: $final (ждали ровно 1)"
  bed=1
else
  echo "  (d) зелёный: ровно один процесс Kelevra.exe остался"
fi

echo "── журнал (хвост) ──"
tail -25 "$ZHURNAL" 2>/dev/null | sed 's/^/  /'

pkill -f "Kelevra.exe" 2>/dev/null

echo "── итог: $([ $bed -eq 0 ] && echo ЗЕЛЁНЫЙ || echo КРАСНЫЙ) ──"
exit $bed
