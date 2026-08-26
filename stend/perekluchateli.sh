#!/usr/bin/env bash
# Стенд «два тумблера окна»: доказывает живьём (настоящая linux-сборка
# приложения, HTTP в живую службу — как жмёт человек, а не вызов функции
# внутри процесса), что ручки
#   POST /api/avtozapusk (internal/sluzhba/sluzhba.go:avtozapuskRuchka)
#   POST /api/avtorezhim (internal/sluzhba/sluzhba.go:avtorezhimRuchka)
# ведут себя так, как обещает internal/sluzhba/oblik/index.html:1098-1120.
#
# Путь человека (oblik/index.html):
#   556-563 — тумблер «Запускать с Windows» ($("perekl-avtozapusk"), строка 1098):
#     клик → POST api/avtozapusk {"vklyuchit": <bool>}; окно берёт состояние
#     из /api/sostoyanie (avtozapusk_podderzhivaetsya/avtozapusk_vklyuchen,
#     892-916), а не запоминает своё — то есть после ЛЮБОГО обновления
#     (obnovit(), включая после перезапуска окна) тумблер обязан рисоваться
#     по тому, что реально лежит на диске у службы.
#   543-551 — тумблер авто-VPN ($("perekl-avtorezhim"), строка 1110):
#     клик → POST api/avtorezhim {"vklyuchit": <bool>}, состояние читается
#     из sostoyanie.avtorezhim_vklyuchen (923-927).
#
# До этого стенда обе ручки были покрыты ТОЛЬКО юнит-тестами внутри процесса
# (internal/sluzhba/avtozapusk_test.go, internal/sluzhba/avtorezhim_test.go)
# — они бьют http.Handler в httptest.NewRecorder(), в обход настоящего TCP,
# и ни один из них не переживает перезапуск ПРОЦЕССА: Sluzhba там одна и та
# же от начала теста до конца, hranenie.Zagruzit() зовётся один раз. Ровно
# этот пробел (17.1КБ.28: «трогали вещь не тем путём, каким её трогает
# человек») стенд и закрывает: он убивает и заново поднимает bin, чтобы
# проверить, что настройка правда лежит на диске (internal/hranenie/hranenie.go
# nastroyki.json), а не только в памяти живого процесса.
#
# avtozapusk на этой машине (Linux) — по коду runtime.GOOS==windows
# (sluzhba.go:431) продукт ЧЕСТНО объявляет тумблер неподдержанным:
# avtozapusk_podderzhivaetsya=false, а сама ручка POST всё равно открыта
# (HTTP-роут не скрывают вместе со строкой в окне) и обязана честно
# отказать текстом avtozapusk.Ne ("автозапуск есть только на Windows"),
# а не притвориться, что сработала. Это НЕ баг — так и задумано
# (internal/avtozapusk/avtozapusk_other.go) — стенд проверяет именно этот
# честный отказ, а не выдуманную работу автозапуска под линуксом.
#
#   stend/perekluchateli.sh                      — все сцены, зелёный прогон
#   stend/perekluchateli.sh --kontrol=a|b|c|d     — намеренно портит ожидание
#     одной сцены, чтобы доказать: стенд умеет краснеть.
set -u
KOREN=$(cd "$(dirname "$0")/.." && pwd)
export PATH="$PATH:/usr/local/go/bin"
export HOME=${HOME:-/root}
export GOCACHE=${GOCACHE:-$HOME/.cache/go-build}

STEND="$KOREN/.stend/perekluchateli"
DOM="$STEND/dom"
BIN="$STEND/kelevra_linux"

KONTROL=""
case "${1:-}" in
  --kontrol=a|--kontrol=b|--kontrol=c|--kontrol=d) KONTROL=${1#--kontrol=} ;;
  "") ;;
  *) echo "аргумент не понят: $1 (жду --kontrol=a|b|c|d)" >&2; exit 2 ;;
esac

VSEGO=4
SCENA_N=0
SLUZHBA_PID=""

shag() { # $1 имя  $2 что замерено  $3 итог(текст)
  SCENA_N=$((SCENA_N + 1))
  printf 'сцена %d/%d: %s — %s — итог: 🟢 %s\n' "$SCENA_N" "$VSEGO" "$1" "$2" "$3"
}

past() { # $1 имя  $2 что замерено  $3 причина провала  [$4 доп. вывод]
  SCENA_N=$((SCENA_N + 1))
  printf 'сцена %d/%d: %s — %s — итог: 🔴 ПРОВАЛ: %s\n' "$SCENA_N" "$VSEGO" "$1" "$2" "$3" >&2
  if [ -n "${4:-}" ]; then
    printf -- '--- разбор ---\n%s\n--------------\n' "$4" >&2
  fi
  exit 1
}

pole() { # pole <json-файл-или--> <ключ>
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
  pkill -KILL -f "$DOM" 2>/dev/null
}
trap pochistit EXIT

# podnyat <лог-файл> — запускает $BIN --sluzhba на изолированном $DOM,
# ждёт строку "KELEVRA-SLUZHBA <url>", пишет её в $URL, PID в $SLUZHBA_PID.
podnyat() {
  local log="$1"
  rm -f "$log"
  KELEVRA_DIR="$DOM" KELEVRA_PRAVA=net KELEVRA_BEZ_OBNOVLENIYA=1 \
    "$BIN" --sluzhba > "$log" 2>&1 &
  SLUZHBA_PID=$!
  URL=""
  for _ in $(seq 1 40); do
    URL=$(grep -m1 '^KELEVRA-SLUZHBA ' "$log" 2>/dev/null | awk '{print $2}')
    [ -n "$URL" ] && break
    kill -0 "$SLUZHBA_PID" 2>/dev/null || break
    sleep 0.25
  done
  if [ -z "$URL" ]; then
    echo "⚫ ПРИБОР МЁРТВ: HTTP-интерфейс приложения не поднялся ($log) — продукт НЕ проверялся" >&2
    cat "$log" >&2
    exit 7
  fi
}

# ostanovit — гасит текущий SLUZHBA_PID (для перезапуска между сценой б и в).
ostanovit() {
  [ -z "$SLUZHBA_PID" ] && return 0
  kill -TERM "$SLUZHBA_PID" 2>/dev/null
  for _ in $(seq 1 20); do
    kill -0 "$SLUZHBA_PID" 2>/dev/null || break
    sleep 0.25
  done
  kill -KILL "$SLUZHBA_PID" 2>/dev/null
  wait "$SLUZHBA_PID" 2>/dev/null
  SLUZHBA_PID=""
}

# --- подготовка: сборка приложения, изолированный дом ----------------------
rm -rf "$STEND"
mkdir -p "$STEND" "$DOM"
if ! ( cd "$KOREN" && go build -o "$BIN" ./cmd/kelevra ) > "$STEND/build_kelevra.log" 2>&1; then
  past "сборка" "go build ./cmd/kelevra" "сборка не прошла" "$(cat "$STEND/build_kelevra.log")"
fi

podnyat "$STEND/sluzhba_1.log"
echo "── площадка готова: pid службы=$SLUZHBA_PID, KELEVRA_DIR=$DOM ──"

# =============================================================================
# сцена а) стартовое честное состояние — ровно то, что окно рисует при
# первом открытии, без единого клика: авто-VPN выключен (значение по
# умолчанию hranenie.Nastroyki.Avtorezhim), автозапуск на linux честно
# объявлен неподдержанным (тумблер в окне спрятан, ryadAvt.hidden, строка 894)
# =============================================================================
SOST0=$(curl -s --max-time 5 "${URL}api/sostoyanie")
AR0=$(printf '%s' "$SOST0" | pole - avtorezhim_vklyuchen)
AZ_POD0=$(printf '%s' "$SOST0" | pole - avtozapusk_podderzhivaetsya)
OZHIDANIE_AR0="false"
[ "$KONTROL" = "a" ] && OZHIDANIE_AR0="true" # порча: требуем, что авто-VPN включён сразу после старта — заведомо не так
if [ "$AR0" != "$OZHIDANIE_AR0" ] || [ "$AZ_POD0" != "false" ]; then
  past "стартовое честное состояние" \
    "avtorezhim_vklyuchen=$OZHIDANIE_AR0, avtozapusk_podderzhivaetsya=false (linux)" \
    "avtorezhim_vklyuchen=$AR0, avtozapusk_podderzhivaetsya=$AZ_POD0" "$SOST0"
fi
shag "стартовое честное состояние" "GET sostoyanie: avtorezhim_vklyuchen=$AR0, avtozapusk_podderzhivaetsya=$AZ_POD0" "как рисует окно при первом открытии"

# =============================================================================
# сцена б) тумблер авто-VPN: цикл вкл→выкл→вкл ровно тем запросом, что шлёт
# клик (index.html:1114, POST api/avtorezhim {"vklyuchit":bool}), и каждый
# раз /api/sostoyanie (тем же путём, каким его читает окно при обновлении)
# сразу отражает новое значение
# =============================================================================
proverit_avtorezhim() { # $1 vklyuchit(true/false)  $2 ожидаемое avtorezhim_vklyuchen
  local vkl="$1" ozhid="$2"
  local kod bT
  kod=$(curl -s -o "$STEND/avtorezhim_post.json" -w '%{http_code}' --max-time 10 \
    -X POST "${URL}api/avtorezhim" -d "{\"vklyuchit\":$vkl}") || kod="000"
  local sost gotovo
  sost=$(curl -s --max-time 5 "${URL}api/sostoyanie")
  gotovo=$(pole "$STEND/avtorezhim_post.json" gotovo)
  bT=$(printf '%s' "$sost" | pole - avtorezhim_vklyuchen)
  if [ "$kod" != "200" ] || [ "$gotovo" != "true" ] || [ "$bT" != "$ozhid" ]; then
    past "тумблер авто-VPN (вкл↔выкл)" \
      "POST avtorezhim {vklyuchit:$vkl} → 200 gotovo:true; следом GET sostoyanie.avtorezhim_vklyuchen=$ozhid" \
      "код $kod, gotovo=$gotovo, avtorezhim_vklyuchen=$bT" "$(cat "$STEND/avtorezhim_post.json")"
  fi
}
proverit_avtorezhim true true
OZHID_POSLE_VYKL="false"
[ "$KONTROL" = "b" ] && OZHID_POSLE_VYKL="true" # порча: требуем, что после выключения тумблер остался включён — обязано провалиться
proverit_avtorezhim false "$OZHID_POSLE_VYKL"
proverit_avtorezhim true true
shag "тумблер авто-VPN (вкл↔выкл)" "POST api/avtorezhim трижды (true→false→true), каждый раз sostoyanie совпал сразу" "клик окна отражается немедленно"

# =============================================================================
# сцена в) состояние авто-VPN переживает перезапуск СЛУЖБЫ (не только
# памяти процесса): тумблер сейчас включён (конец сцены б) → убиваем bin,
# поднимаем заново на том же KELEVRA_DIR → /api/sostoyanie нового процесса
# обязан прочитать то же значение с диска (internal/hranenie/hranenie.go
# nastroyki.json), а не сброситься к «выключено» по умолчанию — окно при
# новом запуске службы (main.go:311, ZapustitAvtorezhim по s.Nastroyki.Avtorezhim)
# должно увидеть тот же тумблер, каким его оставил человек
# =============================================================================
NASTROYKI_FAYL="$DOM/nastroyki.json"
if [ ! -f "$NASTROYKI_FAYL" ]; then
  past "авто-VPN переживает перезапуск службы" "nastroyki.json существует до перезапуска (готовим сцену)" "файла нет: $NASTROYKI_FAYL" ""
fi
if [ "$KONTROL" = "c" ]; then
  rm -f "$NASTROYKI_FAYL" # порча: стираем то, что якобы сохранилось — имитирует «Sohranit не сработал»
fi
ostanovit
podnyat "$STEND/sluzhba_2.log"
SOST_POSLE=$(curl -s --max-time 5 "${URL}api/sostoyanie")
AR_POSLE=$(printf '%s' "$SOST_POSLE" | pole - avtorezhim_vklyuchen)
if [ "$AR_POSLE" != "true" ]; then
  past "авто-VPN переживает перезапуск службы" \
    "новый процесс (pid=$SLUZHBA_PID) сразу читает avtorezhim_vklyuchen=true с диска" \
    "avtorezhim_vklyuchen=$AR_POSLE" "$SOST_POSLE"
fi
shag "авто-VPN переживает перезапуск службы" "старый процесс убит, новый (pid=$SLUZHBA_PID) поднят на том же KELEVRA_DIR, sostoyanie сразу avtorezhim_vklyuchen=true" "не сбросился к выключено по умолчанию"

# =============================================================================
# сцена г) тумблер «Запускать с Windows» на linux: ручка HTTP открыта (index.html
# её просто прячет строкой, avtozapusk_podderzhivaetsya=false), но клик по ней
# (если бы дошёл) обязан вернуть ЧЕСТНЫЙ отказ текстом avtozapusk.Ne — код не
# 200, beda непустая с упоминанием Windows, — а не притвориться успехом, и
# сам процесс службы обязан остаться живым (не упасть, не забрать otdat в панику)
# =============================================================================
KOD_AZ=$(curl -s -o "$STEND/avtozapusk_post.json" -w '%{http_code}' --max-time 10 \
  -X POST "${URL}api/avtozapusk" -d '{"vklyuchit":true}') || KOD_AZ="000"
BEDA_AZ=$(pole "$STEND/avtozapusk_post.json" beda)
# По умолчанию требуем ЧЕСТНЫЙ ОТКАЗ: код не 2xx, beda упоминает Windows.
# --kontrol=d требует обратное (код 200, beda пустая) — на настоящем
# продукте это заведомо не так, сцена обязана провалиться.
UDACHA_HONEST="да"
if [ "$KONTROL" = "d" ]; then
  if [ "$KOD_AZ" != "200" ] || [ -n "$BEDA_AZ" ]; then UDACHA_HONEST="нет"; fi
else
  case "$KOD_AZ" in 2??) UDACHA_HONEST="нет";; esac
  case "$BEDA_AZ" in *Windows*) ;; *) UDACHA_HONEST="нет";; esac
fi
if [ "$UDACHA_HONEST" != "да" ]; then
  past "автозапуск на linux честно отказывает" \
    "код НЕ 2xx, beda содержит «Windows» (не притворяется успехом)" \
    "код $KOD_AZ, beda=«$BEDA_AZ»" "$(cat "$STEND/avtozapusk_post.json")"
fi
if ! kill -0 "$SLUZHBA_PID" 2>/dev/null; then
  past "автозапуск на linux честно отказывает" "процесс службы (pid=$SLUZHBA_PID) жив после отказа" "процесс службы умер" "$(tail -20 "$STEND/sluzhba_2.log")"
fi
SOST_POSLE_AZ_KOD=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "${URL}api/sostoyanie")
if [ "$SOST_POSLE_AZ_KOD" != "200" ]; then
  past "автозапуск на linux честно отказывает" "GET sostoyanie после отказа → 200" "код $SOST_POSLE_AZ_KOD" ""
fi
shag "автозапуск на linux честно отказывает" "POST api/avtozapusk {vklyuchit:true} → код $KOD_AZ, beda=«$BEDA_AZ»; служба (pid=$SLUZHBA_PID) жива" "не притворился успехом, не упал"

if [ -n "$KONTROL" ]; then
  printf '\n(--kontrol=%s должен был провалиться выше — если ты это читаешь, порча не сработала)\n' "$KONTROL"
  exit 1
fi
printf '\nВСЁ ЖИВЬЁМ: %d/%d сцен зелёных. Оба тумблера (avtozapusk, avtorezhim) держат путь человека через HTTP.\n' "$SCENA_N" "$VSEGO"
