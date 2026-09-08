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
# Правка 08.09 (диагноз: беда была не в продукте, а в приборе): архитектура
# с 20.08 (#14, cmd/kelevra/zapusk_windows.go) штатно ДВУХПРОЦЕССНАЯ — процесс
# с окном сам поднимает СЛУЖБУ отдельным unix-процессом (podnyatSluzhbuOtdelno,
# main.go, флаг --sluzhba/KELEVRA_BEZ_OKNA=1). Судить по ОБЩЕМУ числу
# unix-процессов "Kelevra.exe" (как было раньше) значит краснеть на штатной
# паре окно+служба КАЖДЫЙ прогон — гейты (a)/(d) ниже переписаны на счёт по
# РОЛИ процесса (rol_processa), а не по общему счётчику.
#
# КРАСНЫЙ, если хоть один раз выполнено любое из:
#   (a) больше одной СЛУЖБЫ или больше одного ОКНА (по отдельности — не
#       суммарно) живо дольше 2 секунд ПОДРЯД — не разовый всплеск (ожидание
#       смерти старой копии само по себе создаёт короткое перекрытие, это не
#       беда), а зависание за пределами потолка ожидания новой копии (10с,
#       см. srokOzhidaniyaSmeny);
#   (b) СРАЗУ ДВА разных адреса службы (kelevra.log: «служба слушает …»)
#       одновременно отвечают по HTTP — не просто оба когда-либо
#       засветились в журнале (смена адреса при переключении режима сама по
#       себе ожидаема — новый экземпляр слушает новый случайный порт), а
#       именно то, что старый и новый ОБА живы в один и тот же момент;
#   (d) через разумный срок после ответа службы не осталась РОВНО одна пара
#       по ролям (служб >1 или окон >1).
# Отдельно, только как подсказка для человека (не отдельный gate — HWND
# трея меняется и в исправленном мире, это не бага сама по себе), стенд
# печатает, сколько разных «hwnd=» засветилось в журнале.
#
# Гейт площадки (новый, 08.09): под wine нет WebView2 — если копия стартует
# как ОКНО без --tiho (например, после UAC-рестарта), она падает в
# okno_windows.go на «ОТКАЗ: нет компонента WebView2» и зовёт skazat(...),
# а skazat под windows — блокирующий MessageBoxW (skazat_windows.go). Нажать
# «ОК» под wine некому — os.Exit(1) после диалога не наступает никогда, и
# unix-процесс окна висит вечно, весь прогон держа роль «окно» на 2. Это
# беда ПЛОЩАДКИ (нет WebView2 под wine), не продукта: если строка «ОТКАЗ: нет
# компонента WebView2» нашлась в журнале, стенд НЕ выносит вердикт по (a)/(d)
# и выходит кодом 2 (не 0 и не 1), чтобы не читаться ни зелёным, ни бедой.
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

# rol_processa <pid> — "sluzhba" или "okno", по /proc/<pid>/cmdline и
# /proc/<pid>/environ. Замерено живьём 08.09: wine переводит argv PE-образа
# в честный unix cmdline (argv[0] — windows-путь, дальше сами флаги), так что
# "--sluzhba" ищем прямо там же, где main.go его ищет (estArg). Синоним
# KELEVRA_BEZ_OKNA=1 (main.go:73) проверяем через environ на случай, если
# роль задана переменной, а не флагом. Если pid уже умер между pgrep и этим
# чтением — cmdline/environ пустые, оба grep молчат, роль по умолчанию "okno"
# (безопасная сторона: лучше лишний раз заподозрить дубль окна, чем пропустить
# дубль службы под видом молчаливого умолчания).
rol_processa() {
  local pid=$1 cmdline
  cmdline=$(tr '\0' ' ' <"/proc/$pid/cmdline" 2>/dev/null)
  case " $cmdline" in
    *" --sluzhba"*|*" --sluzhba "*)
      printf 'sluzhba'; return ;;
  esac
  if tr '\0' '\n' <"/proc/$pid/environ" 2>/dev/null | grep -qx 'KELEVRA_BEZ_OKNA=1'; then
    printf 'sluzhba'; return
  fi
  printf 'okno'
}

# schitat_po_rolyam — печатает "<окон> <служб>" живых ПРЯМО СЕЙЧАС.
schitat_po_rolyam() {
  local pid rol okon=0 sluzhb=0
  for pid in $(pgrep -x "Kelevra.exe" 2>/dev/null); do
    rol=$(rol_processa "$pid")
    if [ "$rol" = "sluzhba" ]; then
      sluzhb=$((sluzhb + 1))
    else
      okon=$((okon + 1))
    fi
  done
  printf '%s %s' "$okon" "$sluzhb"
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

echo "── слежу за процессами (по ролям) и адресами 10 раз/сек, 15 секунд ──"
ryad=""
max_okon=1
max_sluzhb=1
max_zhivyh_adresov=1
# streak_okon/streak_sluzhb — сколько ПОДРЯД замеров (по 100мс) видели
# больше одного процесса ЭТОЙ роли. Отличаем от разового «>1»: новая, уже
# повышенная копия сама ЖДЁТ смерти старой (zhdatSmenu, cmd/kelevra/
# main.go), и на эти доли секунды, пока старая дожидается своего
# time.Sleep(300ms) и выходит, новая уже существует как unix-процесс, но
# ещё не поднимает ни своего адреса, ни своего трея, — это не беда, а сама
# суть безопасной передачи смены. Бедой это становится, только если
# зависает: старая копия не умерла, а новая всё равно решила не ждать
# (потолок zhdatSmenu — 10 секунд, см. srokOzhidaniyaSmeny) — тогда подряд
# идущих замеров с >1 наберётся на секунды, а не на первые доли секунды
# после ответа.
streak_okon=0
max_streak_okon=0
streak_sluzhb=0
max_streak_sluzhb=0
for _ in $(seq 1 150); do
  read -r okon sluzhb <<<"$(schitat_po_rolyam)"
  ryad="$ryad o${okon}s${sluzhb}"
  [ "$okon" -gt "$max_okon" ] && max_okon=$okon
  [ "$sluzhb" -gt "$max_sluzhb" ] && max_sluzhb=$sluzhb
  if [ "$okon" -gt 1 ]; then
    streak_okon=$((streak_okon + 1))
    [ "$streak_okon" -gt "$max_streak_okon" ] && max_streak_okon=$streak_okon
  else
    streak_okon=0
  fi
  if [ "$sluzhb" -gt 1 ]; then
    streak_sluzhb=$((streak_sluzhb + 1))
    [ "$streak_sluzhb" -gt "$max_streak_sluzhb" ] && max_streak_sluzhb=$streak_sluzhb
  else
    streak_sluzhb=0
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
echo "  ряд по ролям (oN=окон, sN=служб на замер):$ryad"
echo "  максимум одновременно: окон=$max_okon (подряд замеров с >1 окном: $max_streak_okon из 150), служб=$max_sluzhb (подряд замеров с >1 службой: $max_streak_sluzhb из 150), по 100мс каждый"
echo "  максимум одновременно ОТВЕЧАЮЩИХ адресов службы: $max_zhivyh_adresov"

adresov_v_zhurnale=$(grep -oE 'служба слушает http://[^[:space:]]+' "$ZHURNAL" 2>/dev/null | awk '{print $3}' | sort -u | wc -l)
hwnd_v_zhurnale=$(grep -oE 'hwnd=0x[0-9a-f]+' "$ZHURNAL" 2>/dev/null | sort -u | wc -l)
echo "  подсказка: разных адресов за весь прогон в журнале — $adresov_v_zhurnale (смена адреса при переключении режима ожидаема сама по себе)"
echo "  подсказка: разных hwnd трея за весь прогон в журнале — $hwnd_v_zhurnale (новое окно трея при переключении режима ожидаемо само по себе)"

read -r final_okon final_sluzhb <<<"$(schitat_po_rolyam)"
echo "  после settle: окон=$final_okon, служб=$final_sluzhb"

if [ "$max_zhivyh_adresov" -gt 1 ]; then
  echo "  (b) КРАСНЫЙ: одновременно отвечали $max_zhivyh_adresov разных адреса службы — два экземпляра работали бок о бок"
  bed=1
else
  echo "  (b) зелёный: ни разу не отвечало больше одного адреса службы одновременно"
fi

# Гейт площадки: под wine нет WebView2 — копия, стартовавшая как ОКНО (без
# --tiho), падает на этой строке и виснет на MessageBoxW (см. шапку файла).
# Это не беда продукта — не выносим вердикт по (a)/(d), выходим кодом 2.
if grep -q 'ОТКАЗ: нет компонента WebView2' "$ZHURNAL" 2>/dev/null; then
  echo "  ⚠️ ПЛОЩАДКА: нет компонента WebView2 под wine — процесс окна висит на модальном"
  echo "     диалоге (skazat -> MessageBoxW, нажать «ОК» под wine некому). Вердикт по (a)/(d) НЕ выношу."
  echo "── журнал (хвост) ──"
  tail -25 "$ZHURNAL" 2>/dev/null | sed 's/^/  /'
  pkill -f "Kelevra.exe" 2>/dev/null
  if [ "$bed" -eq 1 ]; then
    echo "── итог: КРАСНЫЙ (по (b), не связано с площадкой) ──"
    exit 1
  fi
  echo "── итог: ПЛОЩАДКА — не зелёный и не красный, см. выше ──"
  exit 2
fi

# Порог в 20 замеров (2с) отделяет ожидаемое короткое перекрытие «старая
# дожидается своего time.Sleep(300ms), новая уже существует, но молча ждёт
# её смерти» от настоящего зависания (потолок ожидания у новой копии —
# 10 секунд, см. srokOzhidaniyaSmeny в cmd/kelevra/main.go).
porog_streaka=20
if [ "$max_streak_okon" -gt "$porog_streaka" ] || [ "$max_streak_sluzhb" -gt "$porog_streaka" ]; then
  echo "  (a) КРАСНЫЙ: подряд дольше $((porog_streaka / 10))с жило больше одного процесса по роли (окон streak=$max_streak_okon, служб streak=$max_streak_sluzhb, из 150) — старая копия зависла, а новая не дождалась и всё равно поднялась"
  bed=1
else
  echo "  (a) зелёный: больше одного процесса ОДНОЙ роли было живо не дольше $((porog_streaka / 10))с подряд (макс. окон=$max_okon/streak=$max_streak_okon, служб=$max_sluzhb/streak=$max_streak_sluzhb) — это ожидание смерти старой копии, а не гонка"
fi

if [ "$final_okon" -gt 1 ] || [ "$final_sluzhb" -gt 1 ]; then
  echo "  (d) КРАСНЫЙ: после переключения режима живо окон=$final_okon, служб=$final_sluzhb (ждали ровно по одной)"
  bed=1
else
  echo "  (d) зелёный: ровно одна пара окно+служба (или меньше) осталась"
fi

echo "── журнал (хвост) ──"
tail -25 "$ZHURNAL" 2>/dev/null | sed 's/^/  /'

pkill -f "Kelevra.exe" 2>/dev/null

echo "── итог: $([ $bed -eq 0 ] && echo ЗЕЛЁНЫЙ || echo КРАСНЫЙ) ──"
exit $bed
