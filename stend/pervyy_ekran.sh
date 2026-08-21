#!/usr/bin/env bash
# Стенд «первый экран»: доказывает боевую цепочку ЦЕЛИКОМ — от короткого кода
# доступа, который человек вбивает на первом экране, до настоящего пакета,
# прошедшего через настоящий туннель.
#
# Чем это отличается от stend/zhivoy_trafik.sh. Тот стенд кладёт готовый
# тестовый конфиг прямо на диск и поднимает им ядро — начинает С СЕРЕДИНЫ
# цепочки, после того как код уже как-то превратился в профиль. Здесь
# начало — само значение кода (sluzhba.go:382 kod()), дальше идёт настоящий
# HTTP-разговор с «сервером подписки» (internal/podpiska/podpiska.go: GET
# /k/<код>, откат на /s/<код>), настоящая проверка ответа (ProverKonfig,
# podpiska.go:172-181), настоящая сборка конфига под машину (konfig.Prigotovit)
# и только потом — ядро.
#
# Сеть вовне не нужна и не используется: и «сервер подписки», и «удалённый»
# узел выхода — свои процессы на 127.0.0.1. Узел выхода — НАСТОЯЩИЙ sing-box
# (тот же бинарь .stend/sing-box-linux), поднятый вторым, отдельным процессом,
# как сервер (shadowsocks-вход → прямой выход на локальную же «целевую»
# страницу). Так туннель настоящий шифрованный канал между двумя реальными
# процессами ядра, но целиком внутри машины — не нужно ни чужого сервера,
# ни прав на TUN (LXC его не даст — см. zhivoy_trafik.sh), только сокеты.
#
# 🔴 Профиль тут СВОЙ, синтетический, ни разу не /opt/jarvis-goal/repos/kbox/cfg.json
# (боевой профиль хозяина с его uuid и его узлом) — этот файл здесь не читается
# и не упоминается нигде дальше по коду.
#
#   stend/pervyy_ekran.sh            — зелёный прогон: код рабочий, цепочка целая
#   stend/pervyy_ekran.sh --kontrol  — контроль на честность: сервер подписки
#     отдаёт синтаксически валидный JSON БЕЗ единого узла (outbounds) — ровно
#     то место, которое проверяет podpiska.ProverKonfig. Стенд обязан упасть
#     на этом шаге с ненулевым rc и внятной строкой из настоящего кода
#     приложения, а не зазеленеть.
set -u
KOREN=$(cd "$(dirname "$0")/.." && pwd)
export PATH="$PATH:/usr/local/go/bin"
export HOME=${HOME:-/root}
export GOCACHE=${GOCACHE:-$HOME/.cache/go-build}

BIN_YADRO="$KOREN/.stend/sing-box-linux"
STEND="$KOREN/.stend/pervyy_ekran"
DOM="$STEND/dom"
BIN="$STEND/kelevra_linux"

KONTROL=0
[ "${1:-}" = "--kontrol" ] && KONTROL=1

VSEGO=9
SHAG_N=0
SLUZHBA_PID=""; SRV_PID=""; SUB_PID=""; TARGET_PID=""

shag() { # shag <имя> <что замерено> <итог>
  SHAG_N=$((SHAG_N + 1))
  printf 'шаг %d/%d: %s — %s — итог: %s\n' "$SHAG_N" "$VSEGO" "$1" "$2" "$3"
}

past() { # past <имя> <что замерено> <причина провала> [доп. вывод]
  SHAG_N=$((SHAG_N + 1))
  printf 'шаг %d/%d: %s — %s — итог: ПРОВАЛ: %s\n' "$SHAG_N" "$VSEGO" "$1" "$2" "$3" >&2
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

svobodnyy_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'
}

pochistit() {
  for pid in "$SLUZHBA_PID" "$SRV_PID" "$SUB_PID" "$TARGET_PID"; do
    [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null && kill -TERM "$pid" 2>/dev/null
  done
  sleep 0.3
  for pid in "$SLUZHBA_PID" "$SRV_PID" "$SUB_PID" "$TARGET_PID"; do
    [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null && kill -KILL "$pid" 2>/dev/null
  done
  # подстраховка: сирота ядра клиента (или самой службы), если что-то пошло
  # не по плану раньше штатного отключения — cmdline сирот содержит путь дома.
  pkill -KILL -f "$DOM" 2>/dev/null
  pkill -KILL -f "$STEND/srv" 2>/dev/null
}
trap pochistit EXIT

rm -rf "$STEND"
mkdir -p "$STEND" "$DOM/yadro" "$STEND/srv"

# --- шаг 1: сборка настоящего приложения (не go test) ----------------------
if ! ( cd "$KOREN" && go build -o "$BIN" ./cmd/kelevra ) > "$STEND/build.log" 2>&1; then
  past "сборка" "go build ./cmd/kelevra под linux/$(go env GOARCH 2>/dev/null || echo amd64)" \
    "сборка не прошла" "$(cat "$STEND/build.log")"
fi
if [ ! -f "$BIN_YADRO" ]; then
  past "сборка" "настоящее ядро в $BIN_YADRO" "ядра нет — положи sing-box (linux/amd64) в .stend/sing-box-linux ДО этого стенда"
fi
shag "сборка" "go build ./cmd/kelevra → $BIN, ядро $BIN_YADRO" "$(stat -c%s "$BIN") байт, ядро $(stat -c%s "$BIN_YADRO") байт"

# --- шаг 2: два своих локальных «мира вовне»: целевая страница + серверное ядро
PORT_SUB=$(svobodnyy_port); PORT_TARGET=$(svobodnyy_port); PORT_SRV=$(svobodnyy_port)
PORT_MIXED=$(svobodnyy_port); PORT_CLASH=$(svobodnyy_port)
METKA_CELI="stend-cel-$$-$(date +%s)"
SS_PASS="stend-parol-$$"

python3 -c "
import http.server
METKA = '$METKA_CELI'.encode()
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        with open('$STEND/target_zaprosy.log', 'a') as f:
            f.write('GET ' + self.path + ' from ' + self.client_address[0] + '\n')
        self.send_response(200)
        self.send_header('Content-Length', str(len(METKA)))
        self.end_headers()
        self.wfile.write(METKA)
    def log_message(self, *a): pass
http.server.ThreadingHTTPServer(('127.0.0.1', $PORT_TARGET), H).serve_forever()
" > "$STEND/target.log" 2>&1 &
TARGET_PID=$!

cat > "$STEND/srv/config.json" <<EOF
{
  "log": {"level": "info"},
  "inbounds": [{
    "type": "shadowsocks", "tag": "in-stend",
    "listen": "127.0.0.1", "listen_port": $PORT_SRV,
    "method": "aes-256-gcm", "password": "$SS_PASS"
  }],
  "outbounds": [{"type": "direct", "tag": "direct"}],
  "route": {"final": "direct"}
}
EOF
"$BIN_YADRO" run -c "$STEND/srv/config.json" -D "$STEND/srv" > "$STEND/srv.log" 2>&1 &
SRV_PID=$!

CEL_OTVET=""
for _ in $(seq 1 20); do
  CEL_OTVET=$(curl -s --max-time 2 "http://127.0.0.1:$PORT_TARGET/") && [ -n "$CEL_OTVET" ] && break
  sleep 0.25
done
if [ "$CEL_OTVET" != "$METKA_CELI" ]; then
  past "два мира вовне" "своя целевая страница на 127.0.0.1:$PORT_TARGET отвечает своей меткой" \
    "получено '$CEL_OTVET', ждали '$METKA_CELI'" "$(cat "$STEND/target.log")"
fi
SRV_ZHIV=""
for _ in $(seq 1 20); do
  (exec 3<>"/dev/tcp/127.0.0.1/$PORT_SRV") 2>/dev/null && { exec 3>&-; SRV_ZHIV=1; break; }
  kill -0 "$SRV_PID" 2>/dev/null || break
  sleep 0.25
done
if [ -z "$SRV_ZHIV" ]; then
  past "два мира вовне" "серверное ядро (sing-box, shadowsocks-вход) слушает 127.0.0.1:$PORT_SRV" \
    "порт так и не открылся" "$(cat "$STEND/srv.log")"
fi
shag "два мира вовне" "целевая страница 127.0.0.1:$PORT_TARGET отдала свою метку; серверное ядро (настоящий sing-box, pid=$SRV_PID) слушает shadowsocks на 127.0.0.1:$PORT_SRV" "оба живы"

# --- шаг 3: сервер подписки со своим синтетическим профилем -----------------
# Синтетический профиль клиента: mixed-вход (локальный прокси приложения) и
# shadowsocks-выход прямо на серверное ядро шага 2 — outbound «Соединение»
# первого экрана указывает именно на локальный узел, поднятый этим же стендом.
KOD="stend-kod-$$-$(date +%s)"
export STEND KOD PORT_SUB PORT_TARGET PORT_SRV PORT_MIXED PORT_CLASH SS_PASS
python3 <<'PY'
import json, os
d = os.environ
def put(name, obj):
    with open(os.path.join(d['STEND'], name), 'w') as f:
        json.dump(obj, f, ensure_ascii=False, indent=2)

profil_dobryy = {
    "inbounds": [{
        "type": "mixed", "tag": "mixed-in",
        "listen": "127.0.0.1", "listen_port": int(d['PORT_MIXED']),
        "set_system_proxy": False,
    }],
    "outbounds": [{
        "type": "shadowsocks", "tag": "Server",
        "server": "127.0.0.1", "server_port": int(d['PORT_SRV']),
        "method": "aes-256-gcm", "password": d['SS_PASS'],
    }],
    "route": {"final": "Server"},
    "experimental": {"clash_api": {"external_controller": "127.0.0.1:" + d['PORT_CLASH']}},
}
put('profil_dobryy.json', profil_dobryy)

# --kontrol: сервер подписки жив и отвечает 200, но в теле нет ни одного
# узла — синтаксически валидный JSON, семантически мусор. Ровно то, что
# ловит podpiska.ProverKonfig (podpiska.go:172-181), а не выдумка стенда.
put('profil_slomannyy.json', {"soobshchenie": "сервер подписки сломан: узлов нет"})
PY

PROFIL_FILE="profil_dobryy.json"
[ "$KONTROL" = "1" ] && PROFIL_FILE="profil_slomannyy.json"

python3 -c "
import http.server
KOD = '$KOD'
PUT = '$STEND/$PROFIL_FILE'
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        # первый экран пробует /k/<код>, затем /s/<код> (podpiska.go: Puti) —
        # оба префикса обязаны вести к одному и тому же ответу, иначе второй
        # попавшийся 404 в цикле молча перекрывает настоящую ошибку первого.
        if self.path in ('/k/' + KOD, '/s/' + KOD):
            data = open(PUT, 'rb').read()
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(data)))
            self.end_headers()
            self.wfile.write(data)
        else:
            self.send_response(404)
            self.end_headers()
    def log_message(self, *a): pass
http.server.ThreadingHTTPServer(('127.0.0.1', $PORT_SUB), H).serve_forever()
" > "$STEND/sub.log" 2>&1 &
SUB_PID=$!

SUB_KOD=""
for _ in $(seq 1 20); do
  SUB_KOD=$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "http://127.0.0.1:$PORT_SUB/k/$KOD") && [ "$SUB_KOD" = "200" ] && break
  sleep 0.25
done
if [ "$SUB_KOD" != "200" ]; then
  past "сервер подписки" "GET 127.0.0.1:$PORT_SUB/k/<код> → 200" "код $SUB_KOD" "$(cat "$STEND/sub.log")"
fi
shag "сервер подписки" "127.0.0.1:$PORT_SUB отдаёт на /k/<код> файл $PROFIL_FILE $([ "$KONTROL" = 1 ] && echo '(--kontrol: сломанный, без outbounds)' || echo '(рабочий)')" "поднят"

# --- шаг 4: настоящее приложение, как его видит первый экран ---------------
# Ядро приложение качает само при первом «Подключить», если его нет на
# машине (sluzhba.go: podklyuchit → Yadro.Zagruzit) — здесь оно уже есть
# (кладём символьной ссылкой заранее), чтобы стенд не тянул интернет мимо
# сервера подписки, который и так весь на 127.0.0.1.
ln -sf "$BIN_YADRO" "$DOM/yadro/sing-box"
rm -f "$STEND/sluzhba.log"
KELEVRA_DIR="$DOM" KELEVRA_PODPISKA="127.0.0.1:$PORT_SUB" KELEVRA_SHEMA="http" \
  KELEVRA_PRAVA=net KELEVRA_BEZ_OBNOVLENIYA=1 \
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
  past "приложение" "HTTP-интерфейс поднялся за 10с (лог $STEND/sluzhba.log)" \
    "адрес службы так и не появился (процесс жив: $(kill -0 "$SLUZHBA_PID" 2>/dev/null && echo да || echo нет))" \
    "$(cat "$STEND/sluzhba.log")"
fi
SOST0=$(curl -s --max-time 5 "${URL}api/sostoyanie") || SOST0=""
if [ -z "$SOST0" ]; then
  past "приложение" "GET api/sostoyanie отвечает на $URL" "пустой ответ" ""
fi
shag "приложение" "pid=$SLUZHBA_PID слушает $URL с KELEVRA_PODPISKA=127.0.0.1:$PORT_SUB KELEVRA_SHEMA=http (как задаёт sluzhba.go:66); api/sostoyanie: $SOST0" "поднято"

# --- шаг 5: код скормлен так же, как первый экран (POST /api/kod) ----------
# Никакой особой ветки под --kontrol здесь нет и не должно быть: единственная
# разница между режимами — какой файл отдаёт сервер подписки (шаг 3). Если в
# --kontrol это место вдруг ответит 200 без беды, past() ниже сработает как
# обычно и стенд честно зазеленеет там, где должен покраснеть, — то есть сам
# себя выдаст. Проверять это назначение можно только СНАРУЖИ, по коду выхода
# всего скрипта, а не изнутри его же условием — иначе контроль ничего не
# контролирует.
KOD_KOD=$(curl -s -o "$STEND/otvet_kod.json" -w '%{http_code}' --max-time 35 \
  -X POST "${URL}api/kod" -d "{\"kod\":\"$KOD\"}") || KOD_KOD="000"
BEDA_KOD=$(pole "$STEND/otvet_kod.json" beda 2>/dev/null || true)
if [ "$KOD_KOD" != "200" ] || [ -n "$BEDA_KOD" ]; then
  past "код доступа" "POST api/kod → 200 {gotovo:true}" "код $KOD_KOD, беда: ${BEDA_KOD:-нет тела}" \
    "$(tail -c 3000 "$STEND/sluzhba.log")"
fi
shag "код доступа" "POST ${URL}api/kod {\"kod\":\"$KOD\"} → $KOD_KOD $(cat "$STEND/otvet_kod.json")" "gotovo"

# --- шаг 6: профиль появился на диске ---------------------------------------
PUT_PROFILYA="$DOM/profil.json"
if [ ! -f "$PUT_PROFILYA" ]; then
  past "профиль на диске" "файл $PUT_PROFILYA существует" "файла нет" "$(ls -la "$DOM")"
fi
RAZMER_PROFILYA=$(stat -c%s "$PUT_PROFILYA")
EST_OUTBOUNDS=$(python3 -c "import json,sys; d=json.load(open('$PUT_PROFILYA')); print('да' if d.get('outbounds') else 'нет')")
if [ "$EST_OUTBOUNDS" != "да" ]; then
  past "профиль на диске" "$PUT_PROFILYA содержит непустой outbounds" "outbounds: $EST_OUTBOUNDS" "$(cat "$PUT_PROFILYA")"
fi
shag "профиль на диске" "$PUT_PROFILYA, $RAZMER_PROFILYA байт, outbounds есть" "лежит"

# --- шаг 7: ядро приложения поднято — проверка у самого ядра, не по процессу
PODKL_KOD=$(curl -s -o "$STEND/otvet_podklyuchit.json" -w '%{http_code}' --max-time 85 \
  -X POST "${URL}api/podklyuchit") || PODKL_KOD="000"
BEDA_PODKL=$(pole "$STEND/otvet_podklyuchit.json" beda 2>/dev/null || true)
if [ "$PODKL_KOD" != "200" ] || [ -n "$BEDA_PODKL" ]; then
  past "ядро приложения" "POST api/podklyuchit → 200 {gotovo:true}" "код $PODKL_KOD, беда: ${BEDA_PODKL:-нет тела}" \
    "$(tail -c 3000 "$DOM/yadro/yadro.log" 2>/dev/null)$(printf '\n---kelevra.log---\n')$(tail -c 2000 "$DOM/kelevra.log" 2>/dev/null)"
fi
SOST1=$(curl -s --max-time 5 "${URL}api/sostoyanie")
SOST_VAL=$(printf '%s' "$SOST1" | pole - sost)
if [ "$SOST_VAL" != "rabotaet" ]; then
  past "ядро приложения" "api/sostoyanie.sost == rabotaet" "sost=$SOST_VAL" "$(tail -c 3000 "$DOM/yadro/yadro.log" 2>/dev/null)"
fi
# У самого ядра, напрямую по Clash API из конфига (не через слова приложения):
YADRO_VERSIYA=$(curl -s --max-time 5 "http://127.0.0.1:$PORT_CLASH/version") || YADRO_VERSIYA=""
if [ -z "$YADRO_VERSIYA" ]; then
  past "ядро приложения" "Clash API самого ядра на 127.0.0.1:$PORT_CLASH отвечает /version" "пустой ответ" ""
fi
shag "ядро приложения" "sostoyanie.sost=rabotaet; Clash API САМОГО ядра на 127.0.0.1:$PORT_CLASH: $YADRO_VERSIYA" "rabotaet"

# --- шаг 8: ГЛАВНЫЙ — живой пакет через туннель, проверка ТРЕМЯ каналами ---
DO_TO=$(curl -s --max-time 5 "http://127.0.0.1:$PORT_CLASH/connections")
UP0=$(printf '%s' "$DO_TO" | pole - uploadTotal); UP0=${UP0:-0}
VNIZ0=$(printf '%s' "$DO_TO" | pole - downloadTotal); VNIZ0=${VNIZ0:-0}
STROK_CELI_DO=$(wc -l < "$STEND/target_zaprosy.log" 2>/dev/null || echo 0)

HTTP_KOD=$(curl -s -o "$STEND/otvet_celi.txt" -w '%{http_code}' --max-time 15 \
  -x "http://127.0.0.1:$PORT_MIXED" "http://127.0.0.1:$PORT_TARGET/") || HTTP_KOD="000"
OTVET_CELI=$(cat "$STEND/otvet_celi.txt" 2>/dev/null)

POSLE=$(curl -s --max-time 5 "http://127.0.0.1:$PORT_CLASH/connections")
UP1=$(printf '%s' "$POSLE" | pole - uploadTotal); UP1=${UP1:-0}
VNIZ1=$(printf '%s' "$POSLE" | pole - downloadTotal); VNIZ1=${VNIZ1:-0}
STROK_CELI_POSLE=$(wc -l < "$STEND/target_zaprosy.log" 2>/dev/null || echo 0)

if [ "$HTTP_KOD" != "200" ] || [ "$OTVET_CELI" != "$METKA_CELI" ]; then
  past "живой трафик" "curl -x 127.0.0.1:$PORT_MIXED http://127.0.0.1:$PORT_TARGET/ → 200 с меткой цели" \
    "код $HTTP_KOD, тело '$OTVET_CELI' (ждали '$METKA_CELI')" "$(tail -c 3000 "$DOM/yadro/yadro.log" 2>/dev/null)"
fi
if ! [ "$UP1" -gt "$UP0" ] || ! [ "$VNIZ1" -gt "$VNIZ0" ]; then
  past "живой трафик" "второй канал: счётчики Clash API САМОГО ядра выросли" \
    "up $UP0→$UP1, down $VNIZ0→$VNIZ1 — 200 пришёл, но счётчики ядра молчат" ""
fi
if ! [ "$STROK_CELI_POSLE" -gt "$STROK_CELI_DO" ]; then
  past "живой трафик" "третий канал: у ЦЕЛИ (не у клиента) в журнале появилась новая строка" \
    "строк было $STROK_CELI_DO, стало $STROK_CELI_POSLE — цель запроса не видела" "$(cat "$STEND/target_zaprosy.log" 2>/dev/null)"
fi
shag "живой трафик (ГЛАВНЫЙ)" "прокси 127.0.0.1:$PORT_MIXED → клиентское ядро → shadowsocks → серверное ядро → прямая цель 127.0.0.1:$PORT_TARGET: код 200, метка совпала, счётчики ядра up $UP0→$UP1 / down $VNIZ0→$VNIZ1, журнал цели $STROK_CELI_DO→$STROK_CELI_POSLE" "туннель настоящий"

# --- шаг 9: штатная остановка, ноль сирот -----------------------------------
OTKL_KOD=$(curl -s -o "$STEND/otvet_otklyuchit.json" -w '%{http_code}' --max-time 15 \
  -X POST "${URL}api/otklyuchit") || OTKL_KOD="000"
if [ "$OTKL_KOD" != "200" ]; then
  past "отключение" "POST api/otklyuchit → 200" "код $OTKL_KOD" "$(cat "$STEND/otvet_otklyuchit.json" 2>/dev/null)"
fi
sleep 0.5
YADRO_MERTVO="нет"
if ! curl -s --max-time 2 "http://127.0.0.1:$PORT_CLASH/version" > /dev/null 2>&1; then
  YADRO_MERTVO="да"
fi
if [ "$YADRO_MERTVO" != "да" ]; then
  past "отключение" "Clash API клиентского ядра перестал отвечать после otklyuchit" "всё ещё отвечает — ядро осталось сиротой" ""
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
SLUZHBA_PID=""
kill -TERM "$SRV_PID" "$SUB_PID" "$TARGET_PID" 2>/dev/null
sleep 0.3
kill -KILL "$SRV_PID" "$SUB_PID" "$TARGET_PID" 2>/dev/null
SRV_PID=""; SUB_PID=""; TARGET_PID=""
if pgrep -f "$DOM" > /dev/null 2>&1; then
  past "отключение" "ни одного процесса с путём $DOM не осталось" "остались: $(pgrep -af "$DOM")" ""
fi
OTKRYTY_PORT=""
for p in "$PORT_SUB" "$PORT_TARGET" "$PORT_SRV" "$PORT_MIXED" "$PORT_CLASH"; do
  (exec 3<>"/dev/tcp/127.0.0.1/$p") 2>/dev/null && { exec 3>&-; OTKRYTY_PORT="$p"; break; }
done
if [ -n "$OTKRYTY_PORT" ]; then
  past "отключение" "все 5 своих портов свободны" "порт $OTKRYTY_PORT всё ещё отвечает" ""
fi
shag "отключение" "otklyuchit → клиентское ядро мертво (Clash API молчит), служба вышла по сигналу сама, сирот по $DOM нет, все 5 портов свободны" "чисто"

printf '\nВСЁ ЖИВЬЁМ: %d/%d шагов зелёных, отказов ноль. Код доступа довёл до настоящего пакета через настоящий (локальный) туннель.\n' "$SHAG_N" "$VSEGO"
