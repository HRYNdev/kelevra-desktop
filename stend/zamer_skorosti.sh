#!/usr/bin/env bash
# Стенд «Проверить скорость»: доказывает живьём (настоящая linux-сборка
# приложения, никакого wine — эта кнопка не трогает ничего windows-специфичного),
# что ручка /api/zamerit (internal/sluzhba/sluzhba.go:zamerit) переживает
# любой из узлов, не отвечающий по-человечески.
#
# Путь человека (internal/sluzhba/oblik/index.html:1006-1031): подключился →
# в списке узлов группы «Соединение» (не «sam», узлов >1, podklyucheno)
# появляется кнопка «Проверить скорость» → нажатие шлёт
# POST /api/zamerit {"uzly": [...имена группы...]} → ответ раскладывается в
# u.zaderzhka/u.beda каждого узла. До этого стенда ручка не была покрыта ни
# одним стендом из stend/, ни одним HTTP-тестом — только юнит-тесты на
# internal/yadro/uzly_test.go:90,108, которые бьют Yadro.Zamerit в обход HTTP.
#
# Ядро тут — лже-ядро (cmd/lzhe_yadro), а не настоящий sing-box: три формы
# ответа узла (успех/явный отказ 400/зависание) нужно ставить по заказу, а
# настоящее ядро так не умеет по команде. Лже-ядро честно разбирает
# config.json, который приложение кладёт рядом с ним (как это делает
# internal/yadro/uzly.go:GruppyStatik), и отвечает на GET /proxies и
# GET /proxies/<имя>/delay согласно stend/zamer_scen.json, который этот стенд
# переписывает между сценами БЕЗ перезапуска процесса.
#
# Профиль — internal/konfig/testdata/profil_telefona.json, тот же, что несёт
# internal/sluzhba/uzly_test.go: группа «Соединение» с узлами «Нидерланды» и
# «Комната», Clash API на 127.0.0.1:9090 (лже-ядро слушает ровно там).
#
#   stend/zamer_skorosti.sh            — все сцены, зелёный прогон
#   stend/zamer_skorosti.sh --kontrol=a|b|c|d — намеренно портит ожидание одной
#     сцены (см. past()/KONTROL ниже), чтобы доказать: стенд умеет краснеть,
#     а не просто печатает «зелёный» по привычке.
set -u
KOREN=$(cd "$(dirname "$0")/.." && pwd)
export PATH="$PATH:/usr/local/go/bin"
export HOME=${HOME:-/root}
export GOCACHE=${GOCACHE:-$HOME/.cache/go-build}

STEND="$KOREN/.stend/zamer_skorosti"
DOM="$STEND/dom"
BIN="$STEND/kelevra_linux"
LZHE="$STEND/lzhe_yadro_linux"
PROFIL="$KOREN/internal/konfig/testdata/profil_telefona.json"
SCEN="$DOM/yadro/zamer_scen.json"

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

# zamer_pole <ответ zamerit> <имя узла> <поле> — поле конкретного узла из
# массива {"zamer":[{"imya":...,"zaderzhka":...,"beda":...}, ...]}.
zamer_pole() {
  python3 -c '
import json, sys
d = json.load(open(sys.argv[1]))
imya, klyuch = sys.argv[2], sys.argv[3]
for u in d.get("zamer", []):
    if u.get("imya") == imya:
        v = u.get(klyuch, "")
        print(v if not isinstance(v, bool) else str(v).lower())
        raise SystemExit
print("")
' "$1" "$2" "$3"
}

pishi_scenariy() { # pishi_scenariy <json-тело> — переписывает zamer_scen.json
  printf '%s' "$1" > "$SCEN"
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
  # подстраховка: сирота лже-ядра (или сама служба) — cmdline сирот всегда
  # содержит путь изолированного дома.
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
# KELEVRA_AVTOREZHIM_DNS="127.0.0.1:1" — см. пояснение в stend/proksi.sh:
# контейнер сам отвечает fake-ip подменой, без неё domaSeychas (#78) честно,
# но ложно решает «дома» и podklyuchit не поднимает защиту. Здесь это выглядело
# не красным, а ⚫ «ПРИБОР МЁРТВ» (sost=stoit после podklyuchit) — та же беда,
# что красила пять стендов, просто в другой одежде.
KELEVRA_DIR="$DOM" KELEVRA_PRAVA=net KELEVRA_BEZ_OBNOVLENIYA=1 \
  KELEVRA_AVTOREZHIM_DNS="127.0.0.1:1" \
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

PODKL_KOD=$(curl -s -o "$STEND/podklyuchit.json" -w '%{http_code}' --max-time 30 \
  -X POST "${URL}api/podklyuchit") || PODKL_KOD="000"
BEDA_PODKL=$(pole "$STEND/podklyuchit.json" beda 2>/dev/null || true)
if [ "$PODKL_KOD" != "200" ] || [ -n "$BEDA_PODKL" ]; then
  echo "⚫ ПРИБОР МЁРТВ: POST api/podklyuchit не удался (код $PODKL_KOD, беда: ${BEDA_PODKL:-нет}) — путь человека до кнопки не пройден" >&2
  cat "$STEND/sluzhba.log" >&2
  exit 7
fi
SOST0=$(curl -s --max-time 5 "${URL}api/sostoyanie")
if [ "$(printf '%s' "$SOST0" | pole - sost)" != "rabotaet" ]; then
  echo "⚫ ПРИБОР МЁРТВ: api/sostoyanie.sost != rabotaet после podklyuchit ($SOST0)" >&2
  exit 7
fi
YADRO_PID=$(printf '%s' "$SOST0" | pole - pid)

# --- «путь человека»: список узлов ровно так, как его строит index.html ----
UZLY_JSON=$(curl -s --max-time 5 "${URL}api/uzly")
UZLY=$(python3 -c '
import json, sys
d = json.loads(sys.argv[1])
for g in d.get("gruppy", []):
    if g.get("imya") == "Соединение":
        print(json.dumps([u["imya"] for u in g.get("uzly", [])], ensure_ascii=False))
        raise SystemExit
print("[]")
' "$UZLY_JSON")
if [ "$UZLY" != '["Нидерланды", "Комната"]' ]; then
  echo "⚫ ПРИБОР МЁРТВ: api/uzly не отдал ожидаемые узлы группы «Соединение» (получил: $UZLY, ответ целиком: $UZLY_JSON)" >&2
  exit 7
fi
TELO_ZAMERIT='{"uzly":["Нидерланды","Комната"]}'
echo "── площадка готова: pid службы=$SLUZHBA_PID, pid лже-ядра=$YADRO_PID, узлы группы «Соединение»: $UZLY ──"
echo "── человек жмёт «Проверить скорость»: POST ${URL}api/zamerit $TELO_ZAMERIT ──"

# =============================================================================
# сцена а) обычный замер — на каждый узел вернулась задержка
# =============================================================================
pishi_scenariy '{"delay_ms":{"Нидерланды":31,"Комната":57}}'
[ "$KONTROL" = "a" ] && pishi_scenariy '{"delay_ms":{"Нидерланды":0,"Комната":57}}' # порча: 0 мс = «узел не ответил» (uzly.go:161)
KOD_A=$(curl -s -o "$STEND/zamer_a.json" -w '%{http_code}' --max-time 25 -X POST "${URL}api/zamerit" -d "$TELO_ZAMERIT") || KOD_A="000"
Z_NID=$(zamer_pole "$STEND/zamer_a.json" "Нидерланды" zaderzhka)
Z_KOM=$(zamer_pole "$STEND/zamer_a.json" "Комната" zaderzhka)
B_NID=$(zamer_pole "$STEND/zamer_a.json" "Нидерланды" beda)
B_KOM=$(zamer_pole "$STEND/zamer_a.json" "Комната" beda)
if [ "$KOD_A" != "200" ] || [ -z "$Z_NID" ] || [ "$Z_NID" = "0" ] || [ -n "$B_NID" ] || \
   [ -z "$Z_KOM" ] || [ "$Z_KOM" = "0" ] || [ -n "$B_KOM" ]; then
  past "обычный замер" "оба узла вернули zaderzhka>0 без beda" \
    "код $KOD_A, Нидерланды: zaderzhka=$Z_NID beda=$B_NID; Комната: zaderzhka=$Z_KOM beda=$B_KOM" \
    "$(cat "$STEND/zamer_a.json")"
fi
shag "обычный замер" "POST zamerit → 200, Нидерланды=${Z_NID}мс, Комната=${Z_KOM}мс, beda ни у кого нет" "оба узла живы"

# =============================================================================
# сцена б) один узел мёртв (400 + message) — у него beda с причиной, у
# остального узла задержка на месте, ответ не рушится целиком
# =============================================================================
PRICHINA="An error occurred in the delay test"
pishi_scenariy "{\"delay_ms\":{\"Нидерланды\":44},\"beda\":{\"Комната\":\"$PRICHINA\"}}"
[ "$KONTROL" = "b" ] && pishi_scenariy '{"delay_ms":{"Нидерланды":44,"Комната":44}}' # порча: «мёртвый» узел отвечает как живой
KOD_B=$(curl -s -o "$STEND/zamer_b.json" -w '%{http_code}' --max-time 25 -X POST "${URL}api/zamerit" -d "$TELO_ZAMERIT") || KOD_B="000"
Z_NID=$(zamer_pole "$STEND/zamer_b.json" "Нидерланды" zaderzhka)
B_NID=$(zamer_pole "$STEND/zamer_b.json" "Нидерланды" beda)
B_KOM=$(zamer_pole "$STEND/zamer_b.json" "Комната" beda)
if [ "$KOD_B" != "200" ] || [ -z "$Z_NID" ] || [ "$Z_NID" = "0" ] || [ -n "$B_NID" ] || \
   [ "$B_KOM" != "$PRICHINA" ]; then
  past "мёртвый узел не рушит замер" \
    "Комната: beda=«$PRICHINA»; Нидерланды: zaderzhka>0 без beda; код ответа 200 целиком" \
    "код $KOD_B, Нидерланды: zaderzhka=$Z_NID beda=$B_NID; Комната: beda=$B_KOM" \
    "$(cat "$STEND/zamer_b.json")"
fi
shag "мёртвый узел не рушит замер" "код $KOD_B целиком; Комната: beda=«$B_KOM»; Нидерланды: zaderzhka=${Z_NID}мс" "ответ не обрушен одним узлом"

# =============================================================================
# сцена в) узел зависает дольше клиентского таймаута (6с, uzly.go:146), но
# короче таймаута хендлера (20с) — ответ приходит быстро, а не через 20с
# =============================================================================
pishi_scenariy '{"delay_ms":{"Нидерланды":22},"zavis_sek":{"Комната":9}}'
[ "$KONTROL" = "c" ] && pishi_scenariy '{"delay_ms":{"Нидерланды":22},"zavis_sek":{"Комната":1}}' # порча: узел «отвисает» раньше клиентского таймаута — маскирует зависание под обычную беду
NACHALO=$(date +%s.%N)
KOD_V=$(curl -s -o "$STEND/zamer_v.json" -w '%{http_code}' --max-time 19 -X POST "${URL}api/zamerit" -d "$TELO_ZAMERIT") || KOD_V="000"
KONEC=$(date +%s.%N)
DLITELNOST=$(python3 -c "print(round($KONEC - $NACHALO, 2))")
Z_NID=$(zamer_pole "$STEND/zamer_v.json" "Нидерланды" zaderzhka)
B_NID=$(zamer_pole "$STEND/zamer_v.json" "Нидерланды" beda)
B_KOM=$(zamer_pole "$STEND/zamer_v.json" "Комната" beda)
UKLALSYA=$(python3 -c "print('да' if $DLITELNOST < 15 else 'нет')")
if [ "$KOD_V" != "200" ] || [ -z "$Z_NID" ] || [ "$Z_NID" = "0" ] || [ -n "$B_NID" ] || \
   [ -z "$B_KOM" ] || [ "$UKLALSYA" != "да" ]; then
  past "зависший узел не держит весь замер" \
    "ответ пришёл заметно раньше 20с хендлера (< 15с), Комната получила beda, Нидерланды — живая задержка" \
    "заняло ${DLITELNOST}с (уложился: $UKLALSYA), код $KOD_V, Нидерланды: zaderzhka=$Z_NID beda=$B_NID; Комната: beda=$B_KOM" \
    "$(cat "$STEND/zamer_v.json")"
fi
shag "зависший узел не держит весь замер" "заняло ${DLITELNOST}с (лимит хендлера — 20с), Комната: beda=«$B_KOM», Нидерланды: zaderzhka=${Z_NID}мс" "не повис"

# =============================================================================
# сцена г) замер при остановленном ядре — ручка HTTP открыта, хотя UI кнопку
# прячет: приложение обязано вернуть внятную беду, не упасть и не повиснуть,
# а сам процесс приложения — остаться живым (проверяем pid + повторный
# успешный /api/sostoyanie)
# =============================================================================
OTKL_KOD=$(curl -s -o "$STEND/otklyuchit.json" -w '%{http_code}' --max-time 15 -X POST "${URL}api/otklyuchit") || OTKL_KOD="000"
if [ "$OTKL_KOD" != "200" ]; then
  past "остановленное ядро" "POST api/otklyuchit → 200 (готовим сцену)" "код $OTKL_KOD" "$(cat "$STEND/otklyuchit.json")"
fi
sleep 0.5
if kill -0 "$YADRO_PID" 2>/dev/null; then
  past "остановленное ядро" "процесс лже-ядра pid=$YADRO_PID мёртв после otklyuchit (готовим сцену)" "процесс всё ещё жив" ""
fi
KOD_G=$(curl -s -o "$STEND/zamer_g.json" -w '%{http_code}' --max-time 25 -X POST "${URL}api/zamerit" -d "$TELO_ZAMERIT") || KOD_G="000"
B_NID=$(zamer_pole "$STEND/zamer_g.json" "Нидерланды" beda)
B_KOM=$(zamer_pole "$STEND/zamer_g.json" "Комната" beda)
if [ "$KONTROL" = "d" ]; then
  # порча: требуем, чтобы «беды» не было вовсе — на остановленном ядре это
  # заведомо не так, сцена обязана покраснеть.
  B_NID=""; B_KOM=""
fi
if [ "$KOD_G" != "200" ] || [ -z "$B_NID" ] || [ -z "$B_KOM" ]; then
  past "остановленное ядро возвращает внятную беду" \
    "код 200, у обоих узлов непустая beda (не 0/пусто, не подвисание)" \
    "код $KOD_G, Нидерланды: beda=$B_NID; Комната: beda=$B_KOM" "$(cat "$STEND/zamer_g.json")"
fi
if ! kill -0 "$SLUZHBA_PID" 2>/dev/null; then
  past "процесс приложения жив после беды" "pid службы=$SLUZHBA_PID жив" "процесс службы умер" "$(tail -20 "$STEND/sluzhba.log")"
fi
SOST2=$(curl -s --max-time 5 -w '\n%{http_code}' "${URL}api/sostoyanie")
SOST2_KOD=$(printf '%s' "$SOST2" | tail -1)
if [ "$SOST2_KOD" != "200" ]; then
  past "процесс приложения жив после беды" "повторный GET api/sostoyanie → 200" "код $SOST2_KOD" "$SOST2"
fi
shag "остановленное ядро возвращает внятную беду" \
  "код $KOD_G, Нидерланды: beda=«$B_NID», Комната: beda=«$B_KOM»; служба (pid=$SLUZHBA_PID) жива, api/sostoyanie снова отвечает 200" \
  "ни падения, ни зависания"

if [ -n "$KONTROL" ]; then
  printf '\n(--kontrol=%s должен был провалиться выше — если ты это читаешь, порча не сработала)\n' "$KONTROL"
  exit 1
fi
printf '\nВСЁ ЖИВЬЁМ: %d/%d сцен зелёных. /api/zamerit держит удар на всех формах ответа узла.\n' "$SCENA_N" "$VSEGO"
