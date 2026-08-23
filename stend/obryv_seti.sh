#!/usr/bin/env bash
# Стенд «обрыв сети»: единственный вопрос — восстанавливается ли трафик через
# наше приложение САМ после того, как сеть пропала и вернулась, и за сколько.
#
# Живой баг хозяина: «сеть пропадает (нет даже мобильной), потом возвращается —
# Яндекс.Карты не грузят, банковские приложения плохо грузят. Без нашего
# приложения всё работает нормально». Гипотеза (не факт) — ядро/конфиг не
# пересоздаёт исходящие соединения после возврата сети и/или дозвон висит
# секундами. Здесь эта гипотеза ПРОВЕРЯЕТСЯ замером, а не подтверждается
# рассуждением.
#
# Как устроен обрыв. Два сетевых namespace (ip netns), соединённые veth-парой
# с настоящими IP (не 127.0.0.1 — loopback нельзя «уронить» без обрушения
# всего остального). Клиент (наше приложение + его ядро) живёт в NS_APP,
# сервер-выход + целевая HTTP-страница — в NS_OUT. Обрыв — это
# `ip link set <veth клиента> down` внутри NS_APP: маршрут наружу у клиента
# исчезает целиком, ровно как когда на телефоне гаснет Wi-Fi/моб.сеть — а не
# блокировка порта (та отвечает RST/ICMP, это другая, более мягкая беда).
#
# Профиль клиента — синтетический (как в pervyy_ekran.sh): mixed-inbound +
# один shadowsocks-outbound на сервер-ядро в NS_OUT. Настоящего телефонного
# профиля (internal/konfig/testdata/profil_telefona.json) здесь НЕТ —
# selector/urltest/auto_detect_interface/interrupt_exist_connections не
# участвуют. Это сознательное упрощение и одновременно дыра: если разгадка
# бага именно в этой логике (auto_detect_interface и т.п.), этот стенд её не
# увидит — см. вывод в конце.
#
#   stend/obryv_seti.sh            — через наше приложение (mixed-прокси)
#   stend/obryv_seti.sh --kontrol  — тот же обрыв, БЕЗ приложения: запросы
#     идут напрямую через ту же veth-пару. Проверяет заявление хозяина «без
#     нашего приложения всё восстанавливается нормально» на ТОМ ЖЕ стенде.
set -u
KOREN=$(cd "$(dirname "$0")/.." && pwd)
export PATH="$PATH:/usr/local/go/bin"
export HOME=${HOME:-/root}
export GOCACHE=${GOCACHE:-$HOME/.cache/go-build}

BIN_YADRO="$KOREN/.stend/sing-box-linux"
STEND="$KOREN/.stend/obryv_seti"
DOM="$STEND/dom"
BIN="$STEND/kelevra_linux"

KONTROL=0
[ "${1:-}" = "--kontrol" ] && KONTROL=1
METKA=$([ "$KONTROL" = 1 ] && echo "--kontrol (напрямую, без приложения)" || echo "через приложение")

NS_APP="obryv-app"
NS_OUT="obryv-out"
V_APP="v-app"
V_OUT="v-out"
IP_APP="10.250.90.1"
IP_OUT="10.250.90.2"
PREFIX=30

PORT_TARGET=18080
PORT_SRV=18081
PORT_MIXED=18082
PORT_CLASH=18090
SS_PASS="stend-parol-$$"

VSEGO=6
SHAG_N=0
SLUZHBA_PID=""; SRV_PID=""; TARGET_PID=""

shag() { SHAG_N=$((SHAG_N + 1)); printf 'шаг %d/%d: %s — итог: %s\n' "$SHAG_N" "$VSEGO" "$1" "$2"; }
past() {
  SHAG_N=$((SHAG_N + 1))
  printf 'шаг %d/%d: %s — итог: ПРОВАЛ: %s\n' "$SHAG_N" "$VSEGO" "$1" "$2" >&2
  [ -n "${3:-}" ] && printf -- '--- разбор ---\n%s\n--------------\n' "$3" >&2
  exit 1
}
pole() {
  python3 -c '
import json, sys
src, klyuch = sys.argv[1], sys.argv[2]
d = json.load(open(src)) if src != "-" else json.load(sys.stdin)
v = d.get(klyuch, "")
print(v if not isinstance(v, bool) else str(v).lower())
' "$1" "$2"
}
nsapp() { ip netns exec "$NS_APP" "$@"; }
nsout() { ip netns exec "$NS_OUT" "$@"; }

pochistit() {
  for pid in "$SLUZHBA_PID" "$SRV_PID" "$TARGET_PID"; do
    [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null && kill -TERM "$pid" 2>/dev/null
  done
  sleep 0.3
  for pid in "$SLUZHBA_PID" "$SRV_PID" "$TARGET_PID"; do
    [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null && kill -KILL "$pid" 2>/dev/null
  done
  # $DOM ловит только ядро/службу (KELEVRA_DIR попадает в путь профиля,
  # который виден в argv ядра); srv-процесс живёт под $STEND/srv, не под
  # $DOM — без этой более широкой сети он оставался сиротой (замечено
  # живьём: три висящих sing-box run с srv/config.json после первых прогонов).
  pkill -KILL -f "$STEND" 2>/dev/null
  ip netns del "$NS_APP" 2>/dev/null
  ip netns del "$NS_OUT" 2>/dev/null
}
trap pochistit EXIT
pochistit  # снести хвосты прошлого прогона ДО начала (netns переживают крэш)
mkdir -p "$STEND"

# --- шаг 1: сборка ------------------------------------------------------
if ! ( cd "$KOREN" && go build -o "$BIN" ./cmd/kelevra ) > "$STEND/build.log" 2>&1; then
  past "сборка" "go build ./cmd/kelevra не прошла" "$(cat "$STEND/build.log")"
fi
if [ ! -f "$BIN_YADRO" ]; then
  past "сборка" "ядра нет в $BIN_YADRO"
fi
shag "сборка" "$(stat -c%s "$BIN") байт, ядро $(stat -c%s "$BIN_YADRO") байт"

# --- шаг 2: два network namespace + veth с НАСТОЯЩИМ линком между ними ---
ip netns add "$NS_APP" || past "namespaces" "ip netns add $NS_APP не прошла"
ip netns add "$NS_OUT" || past "namespaces" "ip netns add $NS_OUT не прошла"
ip link add "$V_APP" netns "$NS_APP" type veth peer name "$V_OUT" netns "$NS_OUT" \
  || past "namespaces" "ip link add veth-пары не прошла"
nsapp ip link set lo up
nsout ip link set lo up
nsapp ip addr add "$IP_APP/$PREFIX" dev "$V_APP"
nsout ip addr add "$IP_OUT/$PREFIX" dev "$V_OUT"
nsapp ip link set "$V_APP" up
nsout ip link set "$V_OUT" up
PING_OUT=$(nsapp ping -c1 -W1 "$IP_OUT" 2>&1)
if ! echo "$PING_OUT" | grep -q '1 received'; then
  past "namespaces" "ping $NS_APP → $IP_OUT (через настоящую veth) не прошёл" "$PING_OUT"
fi
shag "namespaces" "$NS_APP($IP_APP) <-veth-> $NS_OUT($IP_OUT), ping настоящий"

# --- шаг 3: цель + серверное ядро внутри NS_OUT ---------------------------
nsout python3 -c "
import http.server
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        body = ('cel-otvet-%d' % __import__('time').time()).encode()
        self.send_response(200)
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def log_message(self, *a): pass
http.server.ThreadingHTTPServer(('$IP_OUT', $PORT_TARGET), H).serve_forever()
" > "$STEND/target.log" 2>&1 &
TARGET_PID=$!

mkdir -p "$STEND/srv"
cat > "$STEND/srv/config.json" <<EOF
{
  "log": {"level": "info"},
  "inbounds": [{
    "type": "shadowsocks", "tag": "in-stend",
    "listen": "$IP_OUT", "listen_port": $PORT_SRV,
    "method": "aes-256-gcm", "password": "$SS_PASS"
  }],
  "outbounds": [{"type": "direct", "tag": "direct"}],
  "route": {"final": "direct"}
}
EOF
nsout "$BIN_YADRO" run -c "$STEND/srv/config.json" -D "$STEND/srv" > "$STEND/srv.log" 2>&1 &
SRV_PID=$!

CEL_OK=""
for _ in $(seq 1 20); do
  nsout curl -s --max-time 2 "http://$IP_OUT:$PORT_TARGET/" | grep -q cel-otvet && { CEL_OK=1; break; }
  sleep 0.25
done
[ -n "$CEL_OK" ] || past "цель+сервер в NS_OUT" "целевая страница $IP_OUT:$PORT_TARGET не ответила" "$(cat "$STEND/target.log")"
SRV_OK=""
for _ in $(seq 1 20); do
  nsout bash -c "exec 3<>/dev/tcp/$IP_OUT/$PORT_SRV" 2>/dev/null && { SRV_OK=1; break; }
  kill -0 "$SRV_PID" 2>/dev/null || break
  sleep 0.25
done
[ -n "$SRV_OK" ] || past "цель+сервер в NS_OUT" "серверное ядро $IP_OUT:$PORT_SRV не открыло порт" "$(cat "$STEND/srv.log")"
shag "цель+сервер в NS_OUT" "target $IP_OUT:$PORT_TARGET жив, серверное ядро (shadowsocks-вход) $IP_OUT:$PORT_SRV жив"

# --- шаг 4: клиент в NS_APP -------------------------------------------------
rm -rf "$DOM"; mkdir -p "$DOM/yadro"
ln -sf "$BIN_YADRO" "$DOM/yadro/sing-box"

if [ "$KONTROL" = 0 ]; then
  python3 - "$STEND/profil.json" "$PORT_MIXED" "$IP_OUT" "$PORT_SRV" "$SS_PASS" "$PORT_CLASH" <<'PY'
import json, sys
put, port_mixed, ip_out, port_srv, ss_pass, port_clash = sys.argv[1:]
profil = {
    "inbounds": [{
        "type": "mixed", "tag": "mixed-in",
        "listen": "127.0.0.1", "listen_port": int(port_mixed),
        "set_system_proxy": False,
    }],
    "outbounds": [{
        "type": "shadowsocks", "tag": "Server",
        "server": ip_out, "server_port": int(port_srv),
        "method": "aes-256-gcm", "password": ss_pass,
    }],
    "route": {"final": "Server"},
    "experimental": {"clash_api": {"external_controller": "127.0.0.1:" + port_clash}},
}
json.dump(profil, open(put, "w"), ensure_ascii=False, indent=2)
PY
  cp "$STEND/profil.json" "$DOM/profil.json"

  rm -f "$STEND/sluzhba.log"
  nsapp env KELEVRA_DIR="$DOM" KELEVRA_PRAVA=net KELEVRA_BEZ_OBNOVLENIYA=1 \
    "$BIN" --sluzhba > "$STEND/sluzhba.log" 2>&1 &
  SLUZHBA_PID=$!

  URL=""
  for _ in $(seq 1 40); do
    URL=$(grep -m1 '^KELEVRA-SLUZHBA ' "$STEND/sluzhba.log" 2>/dev/null | awk '{print $2}')
    [ -n "$URL" ] && break
    kill -0 "$SLUZHBA_PID" 2>/dev/null || break
    sleep 0.25
  done
  [ -n "$URL" ] || past "клиент в NS_APP" "служба не подняла HTTP-интерфейс" "$(cat "$STEND/sluzhba.log")"

  PODKL_KOD=$(nsapp curl -s -o "$STEND/podklyuchit.json" -w '%{http_code}' --max-time 85 \
    -X POST "${URL}api/podklyuchit") || PODKL_KOD="000"
  BEDA=$(pole "$STEND/podklyuchit.json" beda 2>/dev/null || true)
  [ "$PODKL_KOD" = "200" ] && [ -z "$BEDA" ] || \
    past "клиент в NS_APP" "POST api/podklyuchit → код $PODKL_KOD, беда: ${BEDA:-нет тела}" \
      "$(tail -c 3000 "$DOM/yadro/yadro.log" 2>/dev/null)"
  shag "клиент в NS_APP" "приложение pid=$SLUZHBA_PID, mixed-прокси 127.0.0.1:$PORT_MIXED → shadowsocks → $IP_OUT:$PORT_SRV, режим=$(nsapp curl -s "${URL}api/sostoyanie" | pole - rezhim)"
  ZAPROS() { nsapp curl -s -o "$STEND/tmp_resp" -w '%{http_code} %{time_total}' --max-time 6 \
      -x "http://127.0.0.1:$PORT_MIXED" "http://$IP_OUT:$PORT_TARGET/" 2>/dev/null; }
else
  shag "клиент в NS_APP" "--kontrol: приложение НЕ запускается, запросы напрямую $NS_APP → $IP_OUT:$PORT_TARGET через ту же veth"
  ZAPROS() { nsapp curl -s -o "$STEND/tmp_resp" -w '%{http_code} %{time_total}' --max-time 6 \
      "http://$IP_OUT:$PORT_TARGET/" 2>/dev/null; }
fi

# --- шаг 5: сценарий по фазам A(10с норма) / B(20с линк вниз) / C(90с) -----
LOG="$STEND/zaprosy_$([ "$KONTROL" = 1 ] && echo kontrol || echo app).csv"
: > "$LOG"
T0=$(date +%s.%N)
zapisat() { # zapisat <фаза>
  local t=$(date +%s.%N) rc kod vremya ok
  local out; out=$(ZAPROS); rc=$?
  local t2=$(date +%s.%N)
  kod=$(echo "$out" | awk '{print $1}')
  vremya=$(echo "$out" | awk '{print $2}')
  [ "$kod" = "200" ] && grep -q cel-otvet "$STEND/tmp_resp" 2>/dev/null && ok=1 || ok=0
  local dur_ms; dur_ms=$(python3 -c "print(round(($t2 - $t)*1000,1))" 2>/dev/null); [ -n "$dur_ms" ] || dur_ms="?"
  local t_otn; t_otn=$(python3 -c "print(round($t-$T0,2))" 2>/dev/null); [ -n "$t_otn" ] || t_otn="?"
  printf '%s,%s,%s,%s,%s,%s,rc=%s\n' "$1" "$t_otn" "$kod" "$ok" "$dur_ms" "$vremya" "$rc" >> "$LOG"
}

echo "фаза A: 10с нормы ($METKA)"
for _ in $(seq 1 10); do zapisat A; sleep 1; done

T_DOWN=$(python3 -c "import time; print(round(time.time()-$T0,2))")
if ! nsapp ip link set "$V_APP" down; then
  past "обрыв" "ip link set $V_APP down в $NS_APP не прошёл"
fi
LINK_SOST=$(nsapp ip link show "$V_APP" | head -1)
echo "$LINK_SOST" | grep -q ',DOWN' || echo "$LINK_SOST" | grep -qv 'UP' || \
  echo "предупреждение: линк-статус после down: $LINK_SOST"
echo "фаза B: линк $V_APP УПАЛ в $T_DOWN с — 20с обрыва"
for _ in $(seq 1 20); do zapisat B; sleep 0.2; done  # запрос сам держит паузу через --max-time

nsapp ip link set "$V_APP" up
T_UP=$(python3 -c "import time; print(round(time.time()-$T0,2))")
echo "фаза C: линк $V_APP ПОДНЯТ в $T_UP с — приложение НЕ трогаем, 90с долбим"
C_END=$(python3 -c "print($T_UP+90)")
while true; do
  NOW=$(python3 -c "import time; print(round(time.time()-$T0,2))")
  python3 -c "exit(0 if $NOW < $C_END else 1)" || break
  zapisat C
  sleep 1
done

shag "сценарий A/B/C" "лог $LOG, T_down=$T_DOWN T_up=$T_UP"

# --- шаг 6: разбор цифр ------------------------------------------------
python3 - "$LOG" "$T_UP" <<'PY'
import csv, sys
log, t_up = sys.argv[1], float(sys.argv[2])
rows = []
with open(log) as f:
    for line in f:
        parts = line.strip().split(',')
        if len(parts) < 7: continue
        fase, t, kod, ok, dur_ms, vremya, rc = parts
        rows.append((fase, float(t), kod, ok == '1', dur_ms, vremya, rc))

b = [r for r in rows if r[0] == 'B']
c = [r for r in rows if r[0] == 'C']

max_dur_b = max((float(r[4]) for r in b if r[4] not in ('?','')), default=0)
first_ok_c = next((r for r in c if r[3]), None)
fail_streak = 0
for r in c:
    if r[3]: break
    fail_streak += 1

print("=== ФАЗА B (линк вниз) ===")
for r in b:
    print(f"  t={r[1]:>6.2f}с код={r[2]:<4} ok={int(r[3])} длит_curl={r[4]}мс rc={r[6]}")
print(f"  МАКС длительность зависшего запроса в фазе B: {max_dur_b:.0f} мс (это и есть таймаут дозвона, ограничен --max-time=6с)")

print("=== ФАЗА C (линк вверх, приложение не трогали) ===")
for r in c:
    print(f"  t={r[1]:>6.2f}с (+{r[1]-t_up:>6.2f}с от подъёма) код={r[2]:<4} ok={int(r[3])} длит={r[4]}мс rc={r[6]}")

if first_ok_c:
    print(f"\nПЕРВЫЙ успешный ответ после подъёма линка: t={first_ok_c[1]:.2f}с, то есть через {first_ok_c[1]-t_up:.2f}с после T_up")
    print(f"Провалов подряд ДО первого успеха: {fail_streak}")
else:
    print(f"\nНЕ ВОССТАНОВИЛОСЬ за всё окно фазы C ({len(c)} запросов, все провалены)")
PY

# --- лог ядра: всё, что оно сказало о смене сети / пересоздании соединений -
echo "=== из lог ядра ($DOM/yadro/yadro.log), совпадения по сети/интерфейсу/переподключению ==="
grep -iE 'network|interface|route|reconnect|resolve|dial|closed|reset|timeout' "$DOM/yadro/yadro.log" 2>/dev/null | tail -60 || echo "(лога ядра нет — вероятно, --kontrol без приложения)"

printf '\nВСЁ ЖИВЬЁМ ($METKA): %d/%d шагов зелёных.\n' "$SHAG_N" "$VSEGO"
