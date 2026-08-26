#!/usr/bin/env bash
# Стенд «Сохранить» (код доступа): доказывает живьём (настоящая linux-сборка
# приложения), что ручка /api/kod (internal/sluzhba/sluzhba.go:kod) переживает
# любой ответ сервера подписки, не только «сервер всегда отвечает валидным
# конфигом», как это молча предполагали юнит-тесты (internal/sluzhba я не
# нашёл ни одного HTTP-теста на kod вовсе — только internal/podpiska/*_test.go,
# который бьёт Klient напрямую, в обход HTTP-ручки приложения и SohranitProfil).
#
# Путь человека (internal/sluzhba/oblik/index.html:1056-1064): ввёл код в
# «pole-koda» → нажал «Сохранить» → knopka-koda.onclick шлёт
# POST /api/kod {"kod": <значение поля>}.
#
# Сервер подписки тут — свой фейковый HTTP-сервер (fejk_podpiska.py в той же
# папке, что и остальная площадка), подставленный через официальную точку
# подмены KELEVRA_PODPISKA/KELEVRA_SHEMA (internal/sluzhba/sluzhba.go:77-90,
# internal/podpiska/podpiska.go:Host/Shema) — ту же, что использует
# internal/podpiska/podpiska_test.go:klientNa для своих httptest-серверов.
# Три кода — три формы ответа настоящего сервера, которые обязана пережить
# ручка: код принят (валидный config.json), код не существует (404 по обоим
# путям /k/ и /s/), сервер отдаёт мусор вместо конфига (заглушка провайдера,
# HTML вместо JSON). Поведение по коду ABCDGOOD переключается файлом
# podpiska_scen.json (переписывается между сценами без перезапуска фейка),
# остальные коды зашиты в самом фейке — они не меняются между сценами.
#
#   stend/sohranit_kod.sh                — все сцены, зелёный прогон
#   stend/sohranit_kod.sh --kontrol=a|b|c|d — намеренно портит ожидание одной
#     сцены, чтобы доказать: стенд умеет краснеть, а не просто печатает
#     «зелёный» по привычке.
set -u
KOREN=$(cd "$(dirname "$0")/.." && pwd)
export PATH="$PATH:/usr/local/go/bin"
export HOME=${HOME:-/root}
export GOCACHE=${GOCACHE:-$HOME/.cache/go-build}

STEND="$KOREN/.stend/sohranit_kod"
DOM="$STEND/dom"
BIN="$STEND/kelevra_linux"
PROFIL="$KOREN/internal/konfig/testdata/profil_telefona.json"
FEJK_SCEN="$STEND/podpiska_scen.json"
NASTROYKI="$DOM/nastroyki.json"

KOD_DOBRYY="ABCDGOOD"
KOD_NET="NOSUCHKOD"
KOD_MUSOR="MUSORHTML"

KONTROL=""
case "${1:-}" in
  --kontrol=a|--kontrol=b|--kontrol=c|--kontrol=d) KONTROL=${1#--kontrol=} ;;
  "") ;;
  *) echo "аргумент не понят: $1 (жду --kontrol=a|b|c|d)" >&2; exit 2 ;;
esac

VSEGO=4
SCENA_N=0
SLUZHBA_PID=""
FEJK_PID=""

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

seychas_gruppy() { # seychas_gruppy <uzly.json> — есть ли группа «Соединение» и сколько в ней узлов
  python3 -c '
import json, sys
d = json.load(open(sys.argv[1]))
for g in (d.get("gruppy") or []):
    if g.get("imya") == "Соединение":
        print(len(g.get("uzly", [])))
        raise SystemExit
print(0)
' "$1"
}

pishi_fejk_scenariy() { printf '%s' "$1" > "$FEJK_SCEN"; }

pochistit() {
  for pid in "$SLUZHBA_PID" "$FEJK_PID"; do
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
      kill -TERM "$pid" 2>/dev/null
      for _ in $(seq 1 20); do
        kill -0 "$pid" 2>/dev/null || break
        sleep 0.25
      done
      kill -KILL "$pid" 2>/dev/null
    fi
  done
  pkill -KILL -f "$DOM" 2>/dev/null
}
trap pochistit EXIT

# --- подготовка: сборка приложения, изолированный дом ----------------------
rm -rf "$STEND"
mkdir -p "$STEND" "$DOM/yadro"
if ! ( cd "$KOREN" && go build -o "$BIN" ./cmd/kelevra ) > "$STEND/build_kelevra.log" 2>&1; then
  past "сборка" "go build ./cmd/kelevra" "сборка не прошла" "$(cat "$STEND/build_kelevra.log")"
fi
cp "$PROFIL" "$STEND/dobryy_konfig.json"
pishi_fejk_scenariy '{}'

# «без группы» — тот же профиль, но без selector/urltest (см. сцена а,
# --kontrol=a): валиден для konfig.Prigotovit (проходит настоящую сборку
# конфига, не только дешёвую ProverKonfig), но без переключателя «Соединение»
# человеку показать нечего. Собран через ту же функцию, что несёт продукт
# (cmd/zamer_konfig это же и проверяет), не выдуман руками с нуля.
python3 -c '
import json
d = json.load(open("'"$STEND/dobryy_konfig.json"'"))
d["outbounds"] = [o for o in d["outbounds"] if o.get("type") not in ("selector", "urltest")]
json.dump(d, open("'"$STEND/bez_gruppy_konfig.json"'", "w"), ensure_ascii=False)
'

# --- фейковый сервер подписки: GET /k/<kod> и /k/<kod>/info ----------------
# Три кода вшиты (стабильная форма ответа сервера), поведение ABCDGOOD по
# «полный»/«без узлов» переключается podpiska_scen.json — так сцена «а»
# может доказать, что стенд ловит «конфиг валиден по ProverKonfig, но в нём
# нет ожидаемой группы».
cat > "$STEND/fejk_podpiska.py" <<PYEOF
import http.server, json, os, sys

DOM = sys.argv[1]
DOBRYY = sys.argv[2]
NET = sys.argv[3]
MUSOR = sys.argv[4]
SCEN = sys.argv[5]
POLNYY = sys.argv[6]
BEZ_GRUPPY = sys.argv[7]

def chitat_scen():
    try:
        with open(SCEN) as f:
            return json.load(f)
    except Exception:
        return {}

class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def do_GET(self):
        parts = self.path.lstrip("/").split("/")
        if len(parts) < 2 or parts[0] not in ("k", "s"):
            self.send_response(404); self.end_headers(); return
        kod = parts[1]
        if len(parts) >= 3 and parts[2] == "info":
            self.send_response(404); self.end_headers(); return
        if kod == DOBRYY:
            scen = chitat_scen()
            put = BEZ_GRUPPY if scen.get("bez_gruppy") else POLNYY
            with open(put, "rb") as f:
                telo = f.read()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(telo)
            return
        if kod == MUSOR:
            self.send_response(200)
            self.send_header("Content-Type", "text/html")
            self.end_headers()
            self.wfile.write(b"<html>oops, wrong page</html>")
            return
        self.send_response(404); self.end_headers()

srv = http.server.HTTPServer(("127.0.0.1", 0), H)
print(f"FEJK-PODPISKA 127.0.0.1:{srv.server_address[1]}", flush=True)
srv.serve_forever()
PYEOF
rm -f "$STEND/fejk.log"
python3 "$STEND/fejk_podpiska.py" "$DOM" "$KOD_DOBRYY" "$KOD_NET" "$KOD_MUSOR" "$FEJK_SCEN" "$STEND/dobryy_konfig.json" "$STEND/bez_gruppy_konfig.json" > "$STEND/fejk.log" 2>&1 &
FEJK_PID=$!
FEJK_ADRES=""
for _ in $(seq 1 40); do
  FEJK_ADRES=$(grep -m1 '^FEJK-PODPISKA ' "$STEND/fejk.log" 2>/dev/null | awk '{print $2}')
  [ -n "$FEJK_ADRES" ] && break
  kill -0 "$FEJK_PID" 2>/dev/null || break
  sleep 0.1
done
if [ -z "$FEJK_ADRES" ]; then
  echo "⚫ ПРИБОР МЁРТВ: фейковый сервер подписки не поднялся — продукт НЕ проверялся" >&2
  cat "$STEND/fejk.log" >&2
  exit 7
fi

rm -f "$STEND/sluzhba.log"
KELEVRA_DIR="$DOM" KELEVRA_PRAVA=net KELEVRA_BEZ_OBNOVLENIYA=1 \
  KELEVRA_PODPISKA="$FEJK_ADRES" KELEVRA_SHEMA=http \
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
echo "── площадка готова: pid службы=$SLUZHBA_PID, pid фейка подписки=$FEJK_PID на $FEJK_ADRES ──"
echo "── человек жмёт «Сохранить»: POST ${URL}api/kod {\"kod\":\"...\"} ──"

# =============================================================================
# сцена а) обычный код — сервер подписки отдаёт настоящий конфиг: профиль
# реально сохраняется и реально разбирается (не просто HTTP 200 без эффекта)
# =============================================================================
pishi_fejk_scenariy '{}'
[ "$KONTROL" = "a" ] && pishi_fejk_scenariy '{"bez_gruppy":true}' # порча: сервер отдаёт валидный (ProverKonfig проходит), но пустой по смыслу конфиг без группы «Соединение»
KOD_A=$(curl -s -o "$STEND/kod_a.json" -w '%{http_code}' --max-time 35 -X POST "${URL}api/kod" -d "{\"kod\":\"$KOD_DOBRYY\"}") || KOD_A="000"
GOTOVO_A=$(pole "$STEND/kod_a.json" gotovo)
UZLY_A=$(curl -s --max-time 5 "${URL}api/uzly"); printf '%s' "$UZLY_A" > "$STEND/uzly_a.json"
CHISLO_UZLOV_A=$(seychas_gruppy "$STEND/uzly_a.json")
if [ "$KOD_A" != "200" ] || [ "$GOTOVO_A" != "true" ] || [ "$CHISLO_UZLOV_A" != "2" ]; then
  past "обычный код сохраняет рабочий профиль" \
    "код 200, gotovo=true, api/uzly отдаёт группу «Соединение» из 2 узлов" \
    "код $KOD_A, gotovo=$GOTOVO_A, узлов в группе=$CHISLO_UZLOV_A" \
    "$(cat "$STEND/kod_a.json")"
fi
shag "обычный код сохраняет рабочий профиль" "код $KOD_A, gotovo=$GOTOVO_A, api/uzly отдал $CHISLO_UZLOV_A узла(ов) группы «Соединение»" "профиль реально живой, не только HTTP 200"

# =============================================================================
# сцена б) пустое поле — человек нажал «Сохранить», ничего не введя
# =============================================================================
KOD_B=$(curl -s -o "$STEND/kod_b.json" -w '%{http_code}' --max-time 10 -X POST "${URL}api/kod" -d '{"kod":""}') || KOD_B="000"
BEDA_B=$(pole "$STEND/kod_b.json" beda)
[ "$KONTROL" = "b" ] && KOD_B="200" # порча: как будто ручка молча приняла пустой код
if [ "$KOD_B" != "400" ] || [ -z "$BEDA_B" ]; then
  past "пустое поле отвергается понятно" "код 400, непустая beda («пустой код доступа»), сеть не тревожим" "код $KOD_B, beda=$BEDA_B" "$(cat "$STEND/kod_b.json")"
fi
if ! kill -0 "$SLUZHBA_PID" 2>/dev/null; then
  past "процесс приложения жив после пустого поля" "pid службы=$SLUZHBA_PID жив" "процесс службы умер" "$(tail -20 "$STEND/sluzhba.log")"
fi
shag "пустое поле отвергается понятно" "код $KOD_B, beda=«$BEDA_B», служба (pid=$SLUZHBA_PID) жива" "не дошло даже до сети"

# =============================================================================
# сцена в) сервер подписки не знает такой код (опечатка/просроченный код) —
# внятная beda, старый профиль (если был) не тронут
# =============================================================================
KOD_V=$(curl -s -o "$STEND/kod_v.json" -w '%{http_code}' --max-time 20 -X POST "${URL}api/kod" -d "{\"kod\":\"$KOD_NET\"}") || KOD_V="000"
BEDA_V=$(pole "$STEND/kod_v.json" beda)
UZLY_V=$(curl -s --max-time 5 "${URL}api/uzly"); printf '%s' "$UZLY_V" > "$STEND/uzly_v.json"
CHISLO_UZLOV_V=$(seychas_gruppy "$STEND/uzly_v.json")
[ "$KONTROL" = "c" ] && BEDA_V="" # порча: как будто сервер не пожаловался, а ручка проглотила неизвестный код молча
if [ "$KOD_V" != "400" ] || [ -z "$BEDA_V" ] || [ "$CHISLO_UZLOV_V" != "2" ]; then
  past "неизвестный код отвергается, старый профиль цел" \
    "код 400, непустая beda, старый профиль (сцена а) всё ещё виден в api/uzly (2 узла)" \
    "код $KOD_V, beda=$BEDA_V, узлов в группе=$CHISLO_UZLOV_V" \
    "$(cat "$STEND/kod_v.json")"
fi
shag "неизвестный код отвергается, старый профиль цел" "код $KOD_V, beda=«$BEDA_V», старый профиль цел ($CHISLO_UZLOV_V узла)" "сервер сказал «нет», профиль не тронут"

# =============================================================================
# сцена г) сервер подписки отдаёт мусор вместо конфига (например HTML-страницу
# провайдера) — ProverKonfig обязан это поймать, а не скормить мусор ядру
# =============================================================================
pishi_fejk_scenariy '{}'
KOD_G=$(curl -s -o "$STEND/kod_g.json" -w '%{http_code}' --max-time 20 -X POST "${URL}api/kod" -d "{\"kod\":\"$KOD_MUSOR\"}") || KOD_G="000"
BEDA_G=$(pole "$STEND/kod_g.json" beda)
UZLY_G=$(curl -s --max-time 5 "${URL}api/uzly"); printf '%s' "$UZLY_G" > "$STEND/uzly_g.json"
CHISLO_UZLOV_G=$(seychas_gruppy "$STEND/uzly_g.json")
[ "$KONTROL" = "d" ] && BEDA_G="" # порча: как будто мусор прошёл проверку конфига молча
if [ "$KOD_G" != "400" ] || [ -z "$BEDA_G" ] || [ "$CHISLO_UZLOV_G" != "2" ]; then
  past "мусор вместо конфига отвергается, старый профиль цел" \
    "код 400, непустая beda, старый профиль (сцена а) всё ещё виден в api/uzly (2 узла)" \
    "код $KOD_G, beda=$BEDA_G, узлов в группе=$CHISLO_UZLOV_G" \
    "$(cat "$STEND/kod_g.json")"
fi
if ! kill -0 "$SLUZHBA_PID" 2>/dev/null; then
  past "процесс приложения жив после мусора от сервера" "pid службы=$SLUZHBA_PID жив" "процесс службы умер" "$(tail -20 "$STEND/sluzhba.log")"
fi
shag "мусор вместо конфига отвергается, старый профиль цел" "код $KOD_G, beda=«$BEDA_G», старый профиль цел ($CHISLO_UZLOV_G узла), служба (pid=$SLUZHBA_PID) жива" "мусор не дошёл до ядра"

if [ -n "$KONTROL" ]; then
  printf '\n(--kontrol=%s должен был провалиться выше — если ты это читаешь, порча не сработала)\n' "$KONTROL"
  exit 1
fi
printf '\nВСЁ ЖИВЬЁМ: %d/%d сцен зелёных. /api/kod держит удар на всех формах ответа сервера подписки.\n' "$SCENA_N" "$VSEGO"
