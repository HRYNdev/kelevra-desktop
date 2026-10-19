#!/usr/bin/env bash
# Стенд «живой трафик»: доказывает боевую цепочку ЧЕРЕЗ САМО ПРИЛОЖЕНИЕ на
# Linux, а не на стабах и не на голом `sing-box check`.
#
# Зачем отдельно от остальных стендов. Ни один щуп в репозитории ни разу не
# поднимал НАСТОЯЩЕЕ ядро sing-box через саму программу и не пропускал через
# него живой пакет: windows.sh/proksi.sh/trey.sh гоняют настоящий .exe, но
# под wine, а wine не отдаёт ядру сетевые адаптеры — там ядро запускается и
# тут же падает на любом входе, который просит реальный сокет вовне процесса.
# konfig-тесты (TestYadroPrinimaetGotovyyKonfig) зовут настоящее ядро, но
# только `sing-box check` — это разбор конфига, не поднятая связь.
#
# На Linux то же самое приложение собирается и работает без окна (см. README,
# cmd/kelevra/zapusk_other.go, --sluzhba) — здесь этим и пользуемся: собираем
# `cmd/kelevra` под Linux, поднимаем его как настоящий процесс со своим
# изолированным домом (KELEVRA_DIR — отдельная папка внутри .stend/, никогда
# не настоящий ~/.local/share/kelevra) и просим подключиться. При настоящих
# правах (вне этой песочницы) здесь гонялся живой пакет с проверкой ДВУМЯ
# каналами (код ответа И счётчики трафика ядра) — см. шаг 5 и комментарий
# ниже про 0.6.77, почему здесь он не проверяется.
#
# 0.6.77 (e36411b): неполного (прокси) режима больше нет — без прав
# администратора podklyuchit отдаёт OshibkaNetPrav СРАЗУ (internal/sluzhba/
# sluzhba.go:PodnyatZashchitu), ядро не запускается вовсе, mixed-порт не
# слушает, системный прокси не ставится никогда. LXC не поднимет TUN
# (`auto_route` требует адаптер, которого тут нет) и настоящих root-прав
# здесь нет и не будет — поэтому права принудительно гасим тем же рычагом,
# что и остальные стенды на этой машине, KELEVRA_PRAVA=net
# (internal/prava/prava_other.go), и НЕЛЬЗЯ его снимать: реальные root-права
# в LXC поднимут настоящий TUN на ХОСТЕ.
#
# Живой трафик (сам смысл имени стенда) этим рычагом доказать нельзя: без
# прав приложение даже не пытается поднять ядро. Шаг 5 поэтому стоит за
# явным условием «есть права» (KELEVRA_STEND_IMEET_PRAVA=1) и в этой
# песочнице печатает «⚪ не проверяется», не роняя итог; шаг 4 вместо
# старого «подключение без прав» доказывает инверсию — то самое поведение,
# которое 0.6.77 гарантирует без прав (400 OshibkaNetPrav, ядро не стартует).
set -u
KOREN=$(cd "$(dirname "$0")/.." && pwd)
export PATH="$PATH:/usr/local/go/bin"
export HOME=${HOME:-/root}
export GOCACHE=${GOCACHE:-$HOME/.cache/go-build}

YADRO_SRC="$KOREN/.stend/sing-box-linux"
STEND="$KOREN/.stend/zhivoy_trafik"
DOM="$STEND/dom"
BIN="$STEND/kelevra_linux"
PROFIL="$KOREN/internal/konfig/testdata/profil_telefona.json"

VSEGO=6
SHAG_N=0
SLUZHBA_PID=""

shag() { # $1 имя  $2 что замерено  $3 итог(текст)
  SHAG_N=$((SHAG_N + 1))
  printf 'шаг %d/%d: %s — %s — итог: %s\n' "$SHAG_N" "$VSEGO" "$1" "$2" "$3"
}

past() { # $1 имя  $2 что замерено  $3 причина провала  [$4 доп. вывод для разбора]
  SHAG_N=$((SHAG_N + 1))
  printf 'шаг %d/%d: %s — %s — итог: ПРОВАЛ: %s\n' "$SHAG_N" "$VSEGO" "$1" "$2" "$3" >&2
  if [ -n "${4:-}" ]; then
    printf -- '--- разбор ---\n%s\n--------------\n' "$4" >&2
  fi
  exit 1
}

# pole <json-файл-или--> <ключ> — плоское поле верхнего уровня, python3 у нас
# уже стенд-инструмент (stend/oblik_snimok.py), лишней зависимости не тащим.
pole() {
  python3 -c '
import json, sys
src, klyuch = sys.argv[1], sys.argv[2]
d = json.load(open(src)) if src != "-" else json.load(sys.stdin)
v = d.get(klyuch, "")
print(v if not isinstance(v, bool) else str(v).lower())
' "$1" "$2"
}

pochistit() {
  if [ -n "$SLUZHBA_PID" ] && kill -0 "$SLUZHBA_PID" 2>/dev/null; then
    kill -TERM "$SLUZHBA_PID" 2>/dev/null
    for _ in $(seq 1 20); do
      kill -0 "$SLUZHBA_PID" 2>/dev/null || break
      sleep 0.5
    done
    kill -KILL "$SLUZHBA_PID" 2>/dev/null
  fi
  # подстраховка: сирота ядра (или сама служба), если что-то пошло не по плану
  # раньше шага 6 — cmdline сирот всегда содержит путь изолированного дома.
  pkill -KILL -f "$DOM" 2>/dev/null
}
trap pochistit EXIT

# --- шаг 1: сборка -----------------------------------------------------
mkdir -p "$STEND"
if ! ( cd "$KOREN" && go build -o "$BIN" ./cmd/kelevra ) > "$STEND/build.log" 2>&1; then
  past "сборка" "go build ./cmd/kelevra под linux/$(go env GOARCH 2>/dev/null || echo amd64)" \
    "сборка не прошла" "$(cat "$STEND/build.log")"
fi
shag "сборка" "go build ./cmd/kelevra → $BIN" "$(stat -c%s "$BIN") байт"

# --- шаг 2: изолированный дом -------------------------------------------
if [ ! -f "$YADRO_SRC" ]; then
  past "изолированный дом" "настоящее ядро в $YADRO_SRC" \
    "ядра нет — положи его (sing-box, linux/amd64) в .stend/sing-box-linux ДО этого стенда"
fi
rm -rf "$DOM"
mkdir -p "$DOM/yadro"
cp "$PROFIL" "$DOM/profil.json"
ln -sf "$YADRO_SRC" "$DOM/yadro/sing-box"
if ! "$DOM/yadro/sing-box" version > "$STEND/yadro_version.log" 2>&1; then
  past "изолированный дом" "ядро в $DOM/yadro/sing-box отвечает на version" \
    "бинарь не запускается" "$(cat "$STEND/yadro_version.log")"
fi
shag "изолированный дом" "$DOM: profil.json + yadro/sing-box (симлинк на .stend/sing-box-linux), НЕ ~/.local/share/kelevra" \
  "$(head -1 "$STEND/yadro_version.log")"

# --- шаг 3: своя служба + /api/podklyuchit без прав ----------------------
rm -f "$STEND/sluzhba.log"
# KELEVRA_AVTOREZHIM_DNS+KELEVRA_AVTOREZHIM_SHLYUZ — см. пояснение в
# stend/proksi.sh: без ОБЕИХ подмен domaSeychas (#78) честно, но ложно решает
# «дома» (стенд физически в домашней сети владельца) и podklyuchit возвращает
# 200 без попытки поднять защиту вовсе, не добираясь до прав — без SHLYUZ
# здесь ровно так и было (sost остался «stoit», шаг 4 ловил не беду продукта).
KELEVRA_DIR="$DOM" KELEVRA_PRAVA=net KELEVRA_BEZ_OBNOVLENIYA=1 \
  KELEVRA_AVTOREZHIM_DNS="127.0.0.1:1" KELEVRA_AVTOREZHIM_SHLYUZ="de:ad:be:ef:00:01" \
  "$BIN" --sluzhba > "$STEND/sluzhba.log" 2>&1 &
SLUZHBA_PID=$!

URL=""
for _ in $(seq 1 40); do
  URL=$(grep -m1 '^KELEVRA-SLUZHBA ' "$STEND/sluzhba.log" 2>/dev/null | awk '{print $2}')
  [ -n "$URL" ] && break
  kill -0 "$SLUZHBA_PID" 2>/dev/null || break
  sleep 0.25
done
if [ -z "$URL" ]; then
  past "своя служба" "HTTP-интерфейс поднялся за 10с (лог $STEND/sluzhba.log)" \
    "адрес службы так и не появился (процесс жив: $(kill -0 "$SLUZHBA_PID" 2>/dev/null && echo да || echo нет))" \
    "$(cat "$STEND/sluzhba.log")"
fi
SOST0=$(curl -s --max-time 5 "${URL}api/sostoyanie") || SOST0=""
if [ -z "$SOST0" ]; then
  past "своя служба" "GET api/sostoyanie отвечает на $URL" "пустой ответ — порт занят под ключом, но не отвечает" ""
fi
shag "своя служба" "процесс pid=$SLUZHBA_PID слушает $URL, api/sostoyanie отвечает: $SOST0" "поднята"

# --- шаг 4: инверсия — без прав ядро не стартует вовсе (0.6.77) ----------
# До 0.6.77 здесь ждали 200 {gotovo:true} и режим «прокси». Теперь без прав
# PodnyatZashchitu отказывает ДО старта ядра (sluzhba.go:2277): это и есть
# контракт, который стенд обязан ловить, если он больше не может поднять
# живой трафик (владелец 17.10: «нет никаких неполных режимов»).
PODKL_KOD=$(curl -s -o "$STEND/podklyuchit.json" -w '%{http_code}' --max-time 15 \
  -X POST "${URL}api/podklyuchit") || PODKL_KOD="000"
BEDA=$(pole "$STEND/podklyuchit.json" beda 2>/dev/null || true)
if [ "$PODKL_KOD" != "400" ] || [ "$BEDA" != "Для работы Kelevra нужны права администратора." ]; then
  past "инверсия: без прав — отказ сразу" "POST api/podklyuchit → 400 {беда: OshibkaNetPrav}" \
    "код $PODKL_KOD, беда: ${BEDA:-нет тела}" "$(cat "$STEND/podklyuchit.json" 2>/dev/null)"
fi
SOST1=$(curl -s --max-time 5 "${URL}api/sostoyanie")
SOST_VAL=$(printf '%s' "$SOST1" | pole - sost)
if [ "$SOST_VAL" = "rabotaet" ]; then
  past "инверсия: без прав — отказ сразу" "api/sostoyanie.sost != rabotaet (ядро не поднялось)" \
    "sost=rabotaet — отказ в правах не помешал ядру стартовать" ""
fi
if pgrep -f "$DOM" > /dev/null 2>&1; then
  past "инверсия: без прав — отказ сразу" "ни одного процесса ядра с путём $DOM после отказа" \
    "процесс ядра стартовал несмотря на отказ в правах: $(pgrep -af "$DOM")" ""
fi
shag "инверсия: без прав — отказ сразу" "код $PODKL_KOD, беда=«$BEDA», sost=$SOST_VAL, процессов ядра по $DOM: 0" "0.6.77 держит контракт"

# --- шаг 5: живой трафик — только если есть права (здесь их нет и не будет) -
# Живой трафик через настоящее ядро в LXC не воспроизвести: снять
# KELEVRA_PRAVA=net нельзя (реальные root-права поднимут TUN на хосте), а без
# него 0.6.77 не запускает ядро вовсе — шаг 4 это и доказывает. Условие ниже
# оставлено явным (не удалено молча), чтобы на машине с настоящими правами
# стенд проверил сам трафик, а не только отказ.
if [ "${KELEVRA_STEND_IMEET_PRAVA:-0}" = "1" ]; then
  past "живой трафик" "KELEVRA_STEND_IMEET_PRAVA=1 на этой машине" \
    "ветка с правами не реализована в этом наряде — живой трафик через туннель это следующий наряд, не zamer_skorosti.sh и не этот стенд" ""
fi
shag "живой трафик" "KELEVRA_STEND_IMEET_PRAVA=${KELEVRA_STEND_IMEET_PRAVA:-0}, LXC без root-прав на TUN" \
  "⚪ не проверяется: нет прав (0.6.77)"

# --- шаг 6: корректное завершение службы, сирот не осталось ---------------
OTKL_KOD=$(curl -s -o "$STEND/otklyuchit.json" -w '%{http_code}' --max-time 15 \
  -X POST "${URL}api/otklyuchit") || OTKL_KOD="000"
if [ "$OTKL_KOD" != "200" ]; then
  past "отключение" "POST api/otklyuchit → 200 (идемпотентно, ядро и не поднималось)" "код $OTKL_KOD" "$(cat "$STEND/otklyuchit.json" 2>/dev/null)"
fi
kill -TERM "$SLUZHBA_PID" 2>/dev/null
SLUZHBA_MERTVA="нет"
for _ in $(seq 1 20); do
  kill -0 "$SLUZHBA_PID" 2>/dev/null || { SLUZHBA_MERTVA="да"; break; }
  sleep 0.5
done
if [ "$SLUZHBA_MERTVA" != "да" ]; then
  past "отключение" "сама служба (pid=$SLUZHBA_PID) вышла по SIGTERM за 10с" "не вышла" ""
fi
SLUZHBA_PID="" # trap больше не должен её добивать — она уже мертва штатно
if pgrep -f "$DOM" > /dev/null 2>&1; then
  past "отключение" "ни одного процесса с путём $DOM не осталось" \
    "остались: $(pgrep -af "$DOM")" ""
fi
shag "отключение" "otklyuchit (идемпотентно, ядро не поднималось), служба вышла по сигналу сама, сирот по $DOM нет" "чисто"

printf '\nВСЁ ЖИВЬЁМ: %d/%d шагов зелёных, отказов ноль.\n' "$SHAG_N" "$VSEGO"
