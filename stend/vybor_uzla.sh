#!/usr/bin/env bash
# Стенд «Выбор узла»: доказывает живьём (настоящая linux-сборка приложения),
# что ручка /api/vybrat (internal/sluzhba/sluzhba.go:vybrat, в ядре —
# internal/yadro/uzly.go:Vybrat) переживает клик по узлу в любом состоянии
# списка, а не только по «счастливой дорожке» юнит-теста.
#
# Путь человека (internal/sluzhba/oblik/index.html:982-1004): в списке узлов
# группы «Соединение» (не «sam») клик по кнопке узла → onclick шлёт
# POST /api/vybrat {"gruppa": g.imya, "uzel": u.imya}. До этого стенда ручка
# была покрыта только internal/sluzhba/uzly_test.go
# (TestVybratSohranyaetVyborPokaYadroStoitIPerezhivaetPerezapusk) — юнит-тест
# бьёт Sluzhba.vybrat в обход HTTP и никогда не проверял живой Clash API.
#
# Ядро тут — лже-ядро (cmd/lzhe_yadro), а не настоящий sing-box: настоящее
# ядро нельзя заставить по заказу либо отвергнуть узел (404), либо тихо
# принять переключение и не применить его — а обе формы нужны этому стенду.
# Лже-ядро честно разбирает config.json (как это делает
# internal/yadro/uzly.go:GruppyStatik) и держит текущий выбор группы (`now`)
# в памяти между вызовами GET /proxies (vybratNow в cmd/lzhe_yadro/main.go) —
# без этого GET после переключения продолжал бы врать дефолтом, и стенд не
# смог бы поймать «сервер ответил 200, но узел не переключился». Порча
# поведения — через vybrat_scen.json (см. cmd/lzhe_yadro/main.go:vybratScen),
# который этот стенд переписывает между сценами без перезапуска процесса.
#
# Профиль — internal/konfig/testdata/profil_telefona.json (тот же, что несёт
# stend/zamer_skorosti.sh): группа «Соединение», узлы «Нидерланды» (default)
# и «Комната», Clash API на 127.0.0.1:9090.
#
#   stend/vybor_uzla.sh                — все сцены, зелёный прогон
#   stend/vybor_uzla.sh --kontrol=a|b|c|d — намеренно портит ожидание одной
#     сцены, чтобы доказать: стенд умеет краснеть, а не просто печатает
#     «зелёный» по привычке.
set -u
KOREN=$(cd "$(dirname "$0")/.." && pwd)
export PATH="$PATH:/usr/local/go/bin"
export HOME=${HOME:-/root}
export GOCACHE=${GOCACHE:-$HOME/.cache/go-build}

STEND="$KOREN/.stend/vybor_uzla"
DOM="$STEND/dom"
BIN="$STEND/kelevra_linux"
LZHE="$STEND/lzhe_yadro_linux"
PROFIL="$KOREN/internal/konfig/testdata/profil_telefona.json"
SCEN="$DOM/yadro/vybrat_scen.json"
NASTROYKI="$DOM/nastroyki.json"

KONTROL=""
case "${1:-}" in
  --kontrol=a|--kontrol=b|--kontrol=c|--kontrol=d) KONTROL=${1#--kontrol=} ;;
  "") ;;
  *) echo "аргумент не понят: $1 (жду --kontrol=a|b|c|d)" >&2; exit 2 ;;
esac

VSEGO=4
SCENA_N=0
SLUZHBA_PID=""

shag() { SCENA_N=$((SCENA_N + 1)); printf 'сцена %d/%d: %s — %s — итог: 🟢 %s\n' "$SCENA_N" "$VSEGO" "$1" "$2" "$3"; }
past() {
  SCENA_N=$((SCENA_N + 1))
  printf 'сцена %d/%d: %s — %s — итог: 🔴 ПРОВАЛ: %s\n' "$SCENA_N" "$VSEGO" "$1" "$2" "$3" >&2
  if [ -n "${4:-}" ]; then printf -- '--- разбор ---\n%s\n--------------\n' "$4" >&2; fi
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

# seychas_uzla <uzly.json> <имя группы> — что group.seychas прямо сейчас.
seychas_uzla() {
  python3 -c '
import json, sys
d = json.load(open(sys.argv[1]))
for g in d.get("gruppy", []):
    if g.get("imya") == sys.argv[2]:
        print(g.get("seychas", ""))
        raise SystemExit
print("")
' "$1" "$2"
}

# uzly_gruppy <nastroyki.json> <группа> — сохранённый на диске выбор группы.
uzly_gruppy() {
  python3 -c '
import json, sys
try:
    d = json.load(open(sys.argv[1]))
except FileNotFoundError:
    print(""); raise SystemExit
print((d.get("uzly") or {}).get(sys.argv[2], ""))
' "$1" "$2"
}

pishi_scenariy() { printf '%s' "$1" > "$SCEN"; }

pochistit() {
  if [ -n "$SLUZHBA_PID" ] && kill -0 "$SLUZHBA_PID" 2>/dev/null; then
    kill -TERM "$SLUZHBA_PID" 2>/dev/null
    for _ in $(seq 1 20); do
      kill -0 "$SLUZHBA_PID" 2>/dev/null || break
      sleep 0.5
    done
    kill -KILL "$SLUZHBA_PID" 2>/dev/null
  fi
  chmod -R u+w "$DOM" 2>/dev/null
  pkill -KILL -f "$DOM" 2>/dev/null
}
trap pochistit EXIT

# --- подготовка: сборка приложения и лже-ядра, изолированный дом -----------
rm -rf "$STEND"
mkdir -p "$STEND" "$DOM/yadro"
if ! ( cd "$KOREN" && go build -o "$BIN" ./cmd/kelevra ) > "$STEND/build_kelevra.log" 2>&1; then
  past "сборка" "go build ./cmd/kelevra" "сборка не прошла" "$(cat "$STEND/build_kelevra.log")"
fi
if ! ( cd "$KOREN" && go build -o "$LZHE" ./cmd/lzhe_yadro ) > "$STEND/build_lzhe.log" 2>&1; then
  past "сборка" "go build ./cmd/lzhe_yadro" "сборка не прошла" "$(cat "$STEND/build_lzhe.log")"
fi
cp "$PROFIL" "$DOM/profil.json"
ln -sf "$LZHE" "$DOM/yadro/sing-box"
pishi_scenariy '{}'

rm -f "$STEND/sluzhba.log"
KELEVRA_DIR="$DOM" KELEVRA_PRAVA=net KELEVRA_BEZ_OBNOVLENIYA=1 \
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
  echo "⚫ ПРИБОР МЁРТВ: HTTP-интерфейс приложения не поднялся — продукт НЕ проверялся" >&2
  cat "$STEND/sluzhba.log" >&2
  exit 7
fi

PODKL_KOD=$(curl -s -o "$STEND/podklyuchit.json" -w '%{http_code}' --max-time 30 -X POST "${URL}api/podklyuchit") || PODKL_KOD="000"
if [ "$PODKL_KOD" != "200" ]; then
  echo "⚫ ПРИБОР МЁРТВ: POST api/podklyuchit не удался (код $PODKL_KOD) — путь человека до кнопки не пройден" >&2
  cat "$STEND/sluzhba.log" >&2
  exit 7
fi
UZLY0=$(curl -s --max-time 5 "${URL}api/uzly")
printf '%s' "$UZLY0" > "$STEND/uzly0.json"
if [ "$(seychas_uzla "$STEND/uzly0.json" "Соединение")" != "Нидерланды" ]; then
  echo "⚫ ПРИБОР МЁРТВ: после podklyuchit группа «Соединение» не «Нидерланды» по умолчанию — площадка не готова ($UZLY0)" >&2
  exit 7
fi
echo "── площадка готова: pid службы=$SLUZHBA_PID, группа «Соединение», seychas=Нидерланды ──"
echo "── человек кликает по узлу «Комната»: POST ${URL}api/vybrat {\"gruppa\":\"Соединение\",\"uzel\":\"Комната\"} ──"

# =============================================================================
# сцена а) обычный клик при работающем ядре — узел и правда переключается в
# живом Clash API (не только HTTP 200), выбор сохраняется на диске
# =============================================================================
pishi_scenariy '{}'
[ "$KONTROL" = "a" ] && pishi_scenariy '{"ignorirovat":true}' # порча: ядро принимает запрос (204), но не применяет — «now» не меняется
KOD_A=$(curl -s -o "$STEND/vybrat_a.json" -w '%{http_code}' --max-time 15 -X POST "${URL}api/vybrat" -d '{"gruppa":"Соединение","uzel":"Комната"}') || KOD_A="000"
UZLY_A=$(curl -s --max-time 5 "${URL}api/uzly"); printf '%s' "$UZLY_A" > "$STEND/uzly_a.json"
SEYCHAS_A=$(seychas_uzla "$STEND/uzly_a.json" "Соединение")
SOHR_A=$(uzly_gruppy "$NASTROYKI" "Соединение")
GOTOVO_A=$(pole "$STEND/vybrat_a.json" gotovo)
if [ "$KOD_A" != "200" ] || [ "$GOTOVO_A" != "true" ] || [ "$SEYCHAS_A" != "Комната" ] || [ "$SOHR_A" != "Комната" ]; then
  past "обычный клик переключает узел" \
    "код 200, gotovo=true, api/uzly.seychas=Комната, nastroyki.uzly.Соединение=Комната" \
    "код $KOD_A, gotovo=$GOTOVO_A, seychas=$SEYCHAS_A, сохранено=$SOHR_A" \
    "$(cat "$STEND/vybrat_a.json")"
fi
shag "обычный клик переключает узел" "код $KOD_A, seychas стал «$SEYCHAS_A», на диске сохранено «$SOHR_A»" "переключение живое, не только HTTP 200"

# =============================================================================
# сцена б) выбор до «Подключить» (ядро остановлено) — переключать в Clash API
# нечего, но выбор всё равно обязан запомниться на диске (комментарий
# sluzhba.go:280-283): человек выбирает узел раньше, чем нажимает «Подключить»
# =============================================================================
OTKL_KOD=$(curl -s -o "$STEND/otklyuchit.json" -w '%{http_code}' --max-time 15 -X POST "${URL}api/otklyuchit") || OTKL_KOD="000"
if [ "$OTKL_KOD" != "200" ]; then
  past "выбор до подключения запоминается" "POST api/otklyuchit → 200 (готовим сцену)" "код $OTKL_KOD" "$(cat "$STEND/otklyuchit.json")"
fi
# порча: hranenie.Sohranit пишет во временный файл nastroyki.json.tmp и
# переименовывает его — если на его месте лежит символьная ссылка на
# /dev/full, запись падает с «no space left on device» ровно как на
# настоящем переполненном диске (root в этом контейнере игнорирует биты
# прав, поэтому chmod тут бесполезен — это не chmod, это /dev/full).
[ "$KONTROL" = "b" ] && ln -sf /dev/full "$DOM/nastroyki.json.tmp"
KOD_B=$(curl -s -o "$STEND/vybrat_b.json" -w '%{http_code}' --max-time 15 -X POST "${URL}api/vybrat" -d '{"gruppa":"Соединение","uzel":"Нидерланды"}') || KOD_B="000"
GOTOVO_B=$(pole "$STEND/vybrat_b.json" gotovo)
rm -f "$DOM/nastroyki.json.tmp"
SOHR_B=$(uzly_gruppy "$NASTROYKI" "Соединение")
if [ "$KOD_B" != "200" ] || [ "$GOTOVO_B" != "true" ] || [ "$SOHR_B" != "Нидерланды" ]; then
  past "выбор до подключения запоминается" \
    "код 200, gotovo=true (ядро стоит — переключать нечего), nastroyki.uzly.Соединение=Нидерланды" \
    "код $KOD_B, gotovo=$GOTOVO_B, сохранено=$SOHR_B" \
    "$(cat "$STEND/vybrat_b.json")"
fi
shag "выбор до подключения запоминается" "код $KOD_B, gotovo=$GOTOVO_B, при остановленном ядре сохранено «$SOHR_B»" "выбор пережил остановленное ядро"

# --- снова поднимаем ядро для сцен в/г ---
PODKL2_KOD=$(curl -s -o "$STEND/podklyuchit2.json" -w '%{http_code}' --max-time 30 -X POST "${URL}api/podklyuchit") || PODKL2_KOD="000"
if [ "$PODKL2_KOD" != "200" ]; then
  past "площадка для сцен в/г" "повторный POST api/podklyuchit → 200" "код $PODKL2_KOD" "$(cat "$STEND/podklyuchit2.json")"
fi

# =============================================================================
# сцена в) устаревшая кнопка — человек кликнул по узлу, который список уже не
# несёт (профиль обновился под курсором): ядро обязано отказать (404), выбор
# НЕ должен подмениться мусором ни в Clash API, ни на диске
# =============================================================================
pishi_scenariy '{}'
[ "$KONTROL" = "c" ] && pishi_scenariy '{"propustit_proverku":true}' # порча: ядро принимает несуществующий узел без проверки членства
KOD_V=$(curl -s -o "$STEND/vybrat_v.json" -w '%{http_code}' --max-time 15 -X POST "${URL}api/vybrat" -d '{"gruppa":"Соединение","uzel":"Призрак"}') || KOD_V="000"
BEDA_V=$(pole "$STEND/vybrat_v.json" beda)
UZLY_V=$(curl -s --max-time 5 "${URL}api/uzly"); printf '%s' "$UZLY_V" > "$STEND/uzly_v.json"
SEYCHAS_V=$(seychas_uzla "$STEND/uzly_v.json" "Соединение")
if [ "$KOD_V" != "400" ] || [ -z "$BEDA_V" ] || [ "$SEYCHAS_V" = "Призрак" ]; then
  past "устаревшая кнопка не подменяет выбор мусором" \
    "код 400, непустая beda, api/uzly.seychas остаётся «Нидерланды», не «Призрак»" \
    "код $KOD_V, beda=$BEDA_V, seychas=$SEYCHAS_V" \
    "$(cat "$STEND/vybrat_v.json")"
fi
shag "устаревшая кнопка не подменяет выбор мусором" "код $KOD_V, beda=«$BEDA_V», seychas остался «$SEYCHAS_V»" "узел-призрак отвергнут"

# =============================================================================
# сцена г) пустое/битое тело — то, что окно никогда не пошлёт само, но ручка
# открыта HTTP и обязана отказать понятно, а не упасть
# =============================================================================
KOD_G=$(curl -s -o "$STEND/vybrat_g.json" -w '%{http_code}' --max-time 15 -X POST "${URL}api/vybrat" -d '{"gruppa":"Соединение"}') || KOD_G="000"
BEDA_G=$(pole "$STEND/vybrat_g.json" beda)
[ "$KONTROL" = "d" ] && KOD_G="200" # порча: как будто ручка молча приняла тело без узла
if [ "$KOD_G" != "400" ] || [ -z "$BEDA_G" ]; then
  past "пустое тело отвергается понятно" "код 400, непустая beda («не сказано, что и на что переключить»)" "код $KOD_G, beda=$BEDA_G" "$(cat "$STEND/vybrat_g.json")"
fi
if ! kill -0 "$SLUZHBA_PID" 2>/dev/null; then
  past "процесс приложения жив после битого тела" "pid службы=$SLUZHBA_PID жив" "процесс службы умер" "$(tail -20 "$STEND/sluzhba.log")"
fi
shag "пустое тело отвергается понятно" "код $KOD_G, beda=«$BEDA_G», служба (pid=$SLUZHBA_PID) жива" "ни падения, ни молчаливого принятия"

if [ -n "$KONTROL" ]; then
  printf '\n(--kontrol=%s должен был провалиться выше — если ты это читаешь, порча не сработала)\n' "$KONTROL"
  exit 1
fi
printf '\nВСЁ ЖИВЬЁМ: %d/%d сцен зелёных. /api/vybrat держит удар на всех формах клика по узлу.\n' "$SCENA_N" "$VSEGO"
