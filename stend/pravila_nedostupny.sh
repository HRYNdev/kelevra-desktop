#!/usr/bin/env bash
# Стенд «источник правил недоступен»: доказывает живьём (настоящее ядро
# .stend/sing-box-linux, настоящий конфиг из internal/konfig.Prigotovit — не
# самодельный JSON), что первый запуск (или запуск после чистки кэша) не
# вешает человека без связи, если сервер правил не отвечает.
#
# Диагноз 23.08. Боевой профиль несёт 22 route.rule_set (тип remote), все
# качаются с https://subkv.chickenkiller.com/rules/*.srs через detour:"direct"
# (мимо туннеля). Замер настоящим ядром на конфиге, который отдаёт Prigotovit:
#   источник жив,    кеш пуст → старт за 3.3с, mixed-порт открыт;
#   источник мёртв (connection refused), кеш пуст → ядро ПАДАЕТ целиком за
#     0.4с строкой «initialize rule-set[N]: initial rule-set: ...: connect:
#     connection refused» — порт не открывается вовсе;
#   источник молчит (i/o timeout), кеш пуст → то же падение, 5.2с;
#   кеш (remnawave.db) уже наполнен, источник мёртв → старт за 0.04с, всё живо.
# Значит на первом запуске (или после чистки кэша) слабая сеть/DNS/провайдер,
# режущий домен правил, валят приложение целиком — человек видит английскую
# техно-простыню вместо связи. Лечение — konfig.Vybor.BezSetevyhPravil
# (internal/konfig/konfig.go): выбросить rule_set и переставить route.final с
# "direct" на туннельный выход, а не просто выбросить правила (это тихо
# пустило бы трафик мимо VPN — хуже падения).
#
# 24.08: у C (BezSetevyhPravil) есть цена — человек теряет умную
# маршрутизацию насовсем, пока источник правил не отдышится. Замер живьём:
# все 22 .srs вместе весят 495 039 байт (0.47 МБ, ads.srs — 94.7%), 18 из 22
# не менялись с 3 августа — комплект стареет медленно, его можно возить прямо
# в бинаре (internal/pravila) и подставлять вместо remote при беде
# (konfig.Vybor.PravilaIzKomplekta): route.final не переставляется, разбор
# трафика по доменам/подсетям остаётся живым.
#
# Сценарии:
#   A. контроль    — источник жив,  кеш пуст, ничего не взведено → живо.
#   B. беда        — источник мёртв (http://127.0.0.1:1, connection refused),
#                     кеш пуст, ничего не взведено → ядро падает, порт НЕ
#                     открывается. Это ОЖИДАЕМОЕ красное поведение ядра — стенд
#                     фиксирует его как «вот от чего лечим», не как свой провал.
#   C. лечение (упрощённое)  — тот же мёртвый источник, кеш пуст,
#                     BezSetevyhPravil=true → ядро стартует, порт открыт, в
#                     записанном конфиге нет ни одного rule_set и
#                     route.final != "direct".
#   D. лечение (комплект)    — тот же мёртвый источник, кеш пуст, встроенный
#                     комплект разложен на диск и подставлен
#                     (PravilaIzKomplekta) → ядро стартует, порт открыт, в
#                     записанном конфиге все 22 rule_set на месте, все
#                     type:"local", route.final остаётся "direct" — умная
#                     маршрутизация НЕ выродилась в «всё в VPN».
#   E. контроль порчей (обязателен) — тот же сценарий D, но один .srs в
#                     разложенной папке комплекта перезаписан мусором ПОСЛЕ
#                     сборки конфига, ПЕРЕД стартом ядра → ядро обязано
#                     ПАДАТЬ. Без E зелёный D ничего не доказывает: он мог бы
#                     зеленеть и в мире, где ядро тихо игнорирует битый
#                     rule_set (а не читает файл по указанному пути).
#
# Стенд красный (ненулевой код), только если A не поднялось, B неожиданно
# поднялось (значит источник данных для диагноза сам протух и стенд ничего не
# проверяет), C не поднялось / не долечило final, D не поднялось / потеряло
# rule_set или final, либо E НЕ упало (значит комплект не читается взаправду).
#
# Среда: в этой LXC нет /dev/net/tun — гоняем только «Проксирование»
# (Vybor.Prava=false), туннель не поднимаем. Порты — из диапазона 224xx,
# чтобы не столкнуться с портами соседних стендов.
set -u
KOREN=$(cd "$(dirname "$0")/.." && pwd)
export PATH="$PATH:/usr/local/go/bin"
export HOME=${HOME:-/root}
export GOCACHE=${GOCACHE:-/tmp/gocache}

YADRO="$KOREN/.stend/sing-box-linux"
STEND="$KOREN/.stend/pravila_nedostupny"
PROFIL_ISHODNIK="$KOREN/internal/konfig/testdata/profil_telefona.json"
ZAMER="$STEND/zamer_konfig"
bed=0

if [ ! -x "$YADRO" ]; then
  echo "⚫ ПРИБОР МЁРТВ: нет настоящего ядра ($YADRO) — стенду нечем гонять сценарии" >&2
  exit 7
fi

rm -rf "$STEND"
mkdir -p "$STEND"

echo "── сборка cmd/zamer_konfig (собирает конфиг настоящим кодом приложения) ──"
if ! ( cd "$KOREN" && go build -o "$ZAMER" ./cmd/zamer_konfig ) > "$STEND/build.log" 2>&1; then
  echo "  НЕ СОБРАЛСЯ:"; cat "$STEND/build.log"; exit 1
fi

# gotov_profil <vyhod.json> <port> <mertvyy 0|1> — копия боевого профиля с
# переставленным mixed-портом (224xx) и, если mertvyy=1, все route.rule_set[].url
# заменены на мёртвый адрес (connect: connection refused — никто не слушает).
gotov_profil() {
  python3 - "$PROFIL_ISHODNIK" "$1" "$2" "$3" <<'PY'
import json, sys
vhod, vyhod, port, mertvyy = sys.argv[1], sys.argv[2], int(sys.argv[3]), sys.argv[4] == "1"
d = json.load(open(vhod))
for v in d.get("inbounds", []):
    if v.get("type") == "mixed":
        v["listen_port"] = port
if mertvyy:
    for rs in d.get("route", {}).get("rule_set", []):
        rs["url"] = "http://127.0.0.1:1/mertv.srs"
json.dump(d, open(vyhod, "w"))
PY
}

# sobrat_konfig <profil.json> <konfig.json> <bez_pravil 0|1> [komplekt_dir] —
# зовёт настоящий Prigotovit (не самодельный JSON). BezSistemnogoProksi всегда
# взведён: на Linux ядро без него падает строкой «initialize system proxy»
# (отдельная, не наша беда) — она бы замаскировала ровно то падение, которое
# мы мерим. komplekt_dir, если задан, зовёт настоящий pravila.Razlozhit
# (internal/pravila) и подставляет Vybor.PravilaIzKomplekta — главнее bez_pravil.
sobrat_konfig() {
  local profil=$1 konfig=$2 bez_pravil=$3 komplekt_dir=${4:-}
  local log="$STEND/zamer_$(basename "$konfig").log"
  local flag=""
  [ "$bez_pravil" = "1" ] && flag="-bez-pravil"
  [ -n "$komplekt_dir" ] && flag="$flag -komplekt-dir $komplekt_dir"
  if ! "$ZAMER" -profil "$profil" -prava=false -bez-proksi $flag > "$konfig" 2> "$log"; then
    echo "  КРАСНЫЙ: cmd/zamer_konfig не собрал конфиг:"; cat "$log"; bed=1; return 1
  fi
  cat "$log"
  return 0
}

# port_otkryt <port> — 0, если mixed-порт принимает соединение прямо сейчас.
port_otkryt() {
  (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null && { exec 3<&- 3>&-; return 0; }
  return 1
}

# zapustit_yadro <workdir> <konfig.json> <log> -> печатает PID в stdout.
# workdir — он же -D (кеш remnawave.db резолвится относительно него): пустой
# каталог на каждый сценарий значит «кеш пуст», как того требует диагноз.
zapustit_yadro() {
  mkdir -p "$1"
  "$YADRO" run -c "$2" -D "$1" > "$3" 2>&1 &
  echo $!
}

# zhdat <pid> <port> <taimaut_sek> -> "otkryt" | "umer" | "taimaut"
zhdat() {
  local pid=$1 port=$2 shagov=$(($3 * 2)) i=0
  while [ "$i" -lt "$shagov" ]; do
    kill -0 "$pid" 2>/dev/null || { echo umer; return; }
    port_otkryt "$port" && { echo otkryt; return; }
    sleep 0.5
    i=$((i + 1))
  done
  echo taimaut
}

pogasit() { # $1 pid
  kill -0 "$1" 2>/dev/null || return 0
  kill -TERM "$1" 2>/dev/null
  for _ in $(seq 1 10); do
    kill -0 "$1" 2>/dev/null || return 0
    sleep 0.3
  done
  kill -KILL "$1" 2>/dev/null
}
trap 'pkill -KILL -f "$STEND" 2>/dev/null' EXIT

echo
echo "── A. контроль: источник правил живой, кеш пуст ──"
PORT_A=22410
gotov_profil "$STEND/profil_a.json" "$PORT_A" 0
sobrat_konfig "$STEND/profil_a.json" "$STEND/konfig_a.json" 0 || true
if [ -s "$STEND/konfig_a.json" ]; then
  PID_A=$(zapustit_yadro "$STEND/dom_a" "$STEND/konfig_a.json" "$STEND/yadro_a.log")
  ITOG_A=$(zhdat "$PID_A" "$PORT_A" 90)
  echo "  итог: $ITOG_A (порт $PORT_A, pid $PID_A)"
  if [ "$ITOG_A" != "otkryt" ]; then
    echo "  КРАСНЫЙ: контроль обязан подняться (источник правил живой) — не поднялся"
    tail -20 "$STEND/yadro_a.log"
    bed=1
  else
    echo "  зелёный: ядро стартовало, порт открыт"
  fi
  pogasit "$PID_A"
fi

echo
echo "── B. беда: источник правил мёртв (connection refused), кеш пуст, BezSetevyhPravil=false ──"
PORT_B=22411
gotov_profil "$STEND/profil_b.json" "$PORT_B" 1
sobrat_konfig "$STEND/profil_b.json" "$STEND/konfig_b.json" 0 || true
if [ -s "$STEND/konfig_b.json" ]; then
  PID_B=$(zapustit_yadro "$STEND/dom_b" "$STEND/konfig_b.json" "$STEND/yadro_b.log")
  ITOG_B=$(zhdat "$PID_B" "$PORT_B" 15)
  echo "  итог: $ITOG_B (порт $PORT_B, pid $PID_B)"
  echo "  хвост журнала ядра:"; tail -5 "$STEND/yadro_b.log" | sed 's/^/    /'
  if [ "$ITOG_B" = "otkryt" ]; then
    echo "  КРАСНЫЙ: ожидали падение ядра без источника правил, а порт открылся — предпосылка диагноза протухла, сценарий C ничего не докажет"
    bed=1
  else
    echo "  ожидаемое красное поведение ЯДРА зафиксировано ($ITOG_B): вот от чего лечим, это не провал стенда"
  fi
  pogasit "$PID_B"
fi

echo
echo "── C. лечение: тот же мёртвый источник, кеш пуст, BezSetevyhPravil=true ──"
PORT_C=22412
gotov_profil "$STEND/profil_c.json" "$PORT_C" 1
sobrat_konfig "$STEND/profil_c.json" "$STEND/konfig_c.json" 1 || true
if [ -s "$STEND/konfig_c.json" ]; then
  RAZBOR=$(python3 - "$STEND/konfig_c.json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
final = d.get("route", {}).get("final", "")
print(final)
print("1" if "rule_set" in json.dumps(d) else "0")
PY
  )
  FINAL_C=$(echo "$RAZBOR" | sed -n 1p)
  EST_RULE_SET=$(echo "$RAZBOR" | sed -n 2p)
  echo "  конфиг: route.final=$FINAL_C, встречается ли ещё \"rule_set\" в файле: $EST_RULE_SET"
  if [ -z "$FINAL_C" ] || [ "$FINAL_C" = "direct" ]; then
    echo "  КРАСНЫЙ: route.final=$FINAL_C — трафик пойдёт мимо VPN молча"; bed=1
  elif [ "$EST_RULE_SET" != "0" ]; then
    echo "  КРАСНЫЙ: rule_set всё ещё встречается в записанном конфиге"; bed=1
  else
    echo "  конфиг вылечен: final указывает на туннельный выход, rule_set нет"
  fi

  PID_C=$(zapustit_yadro "$STEND/dom_c" "$STEND/konfig_c.json" "$STEND/yadro_c.log")
  ITOG_C=$(zhdat "$PID_C" "$PORT_C" 20)
  echo "  итог: $ITOG_C (порт $PORT_C, pid $PID_C)"
  if [ "$ITOG_C" != "otkryt" ]; then
    echo "  КРАСНЫЙ: с BezSetevyhPravil=true ядро обязано подняться даже с мёртвым источником правил — не поднялось"
    tail -20 "$STEND/yadro_c.log"
    bed=1
  else
    echo "  зелёный: ядро стартовало БЕЗ источника правил, порт открыт — деградация лечит"
  fi
  pogasit "$PID_C"
fi

echo
echo "── D. лечение (комплект): тот же мёртвый источник, кеш пуст, встроенный комплект разложен ──"
PORT_D=22413
KOMPLEKT_D="$STEND/komplekt_d"
gotov_profil "$STEND/profil_d.json" "$PORT_D" 1
sobrat_konfig "$STEND/profil_d.json" "$STEND/konfig_d.json" 0 "$KOMPLEKT_D" || true
if [ -s "$STEND/konfig_d.json" ]; then
  RAZBOR=$(python3 - "$STEND/konfig_d.json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
r = d.get("route", {})
final = r.get("final", "")
rs = r.get("rule_set", [])
tipy = sorted(set(x.get("type", "?") for x in rs))
print(final)
print(len(rs))
print(",".join(tipy))
PY
  )
  FINAL_D=$(echo "$RAZBOR" | sed -n 1p)
  KOL_RULE_SET_D=$(echo "$RAZBOR" | sed -n 2p)
  TIPY_D=$(echo "$RAZBOR" | sed -n 3p)
  echo "  конфиг: route.final=$FINAL_D, rule_set=$KOL_RULE_SET_D шт, типы=$TIPY_D"
  if [ "$FINAL_D" != "direct" ]; then
    echo "  КРАСНЫЙ: route.final=$FINAL_D — комплект не должен трогать final, умная маршрутизация выродилась"; bed=1
  elif [ "$KOL_RULE_SET_D" != "22" ]; then
    echo "  КРАСНЫЙ: в конфиге $KOL_RULE_SET_D rule_set вместо 22"; bed=1
  elif [ "$TIPY_D" != "local" ]; then
    echo "  КРАСНЫЙ: не все rule_set стали type:local (видел: $TIPY_D)"; bed=1
  else
    echo "  конфиг вылечен: все 22 rule_set локальные, final не трогали"
  fi

  PID_D=$(zapustit_yadro "$STEND/dom_d" "$STEND/konfig_d.json" "$STEND/yadro_d.log")
  ITOG_D=$(zhdat "$PID_D" "$PORT_D" 20)
  echo "  итог: $ITOG_D (порт $PORT_D, pid $PID_D)"
  if [ "$ITOG_D" != "otkryt" ]; then
    echo "  КРАСНЫЙ: со встроенным комплектом ядро обязано подняться даже с мёртвым источником правил — не поднялось"
    tail -20 "$STEND/yadro_d.log"
    bed=1
  else
    echo "  зелёный: ядро стартовало на встроенном комплекте, порт открыт — умная маршрутизация сохранена"
  fi
  pogasit "$PID_D"
fi

echo
echo "── E. контроль порчей (обязателен): тот же комплект, но один .srs испорчен ПОСЛЕ сборки конфига ──"
PORT_E=22414
KOMPLEKT_E="$STEND/komplekt_e"
gotov_profil "$STEND/profil_e.json" "$PORT_E" 1
sobrat_konfig "$STEND/profil_e.json" "$STEND/konfig_e.json" 0 "$KOMPLEKT_E" || true
if [ -s "$STEND/konfig_e.json" ]; then
  ISPORCHEN=$(ls "$KOMPLEKT_E"/*.srs 2>/dev/null | head -1)
  if [ -z "$ISPORCHEN" ]; then
    echo "  КРАСНЫЙ: комплект не разложился в $KOMPLEKT_E — портить нечего"
    bed=1
  else
    printf 'мусор-вместо-правила-24.08' > "$ISPORCHEN"
    echo "  испорчен файл: $ISPORCHEN"
    PID_E=$(zapustit_yadro "$STEND/dom_e" "$STEND/konfig_e.json" "$STEND/yadro_e.log")
    ITOG_E=$(zhdat "$PID_E" "$PORT_E" 15)
    echo "  итог: $ITOG_E (порт $PORT_E, pid $PID_E)"
    echo "  хвост журнала ядра:"; tail -5 "$STEND/yadro_e.log" | sed 's/^/    /'
    if [ "$ITOG_E" = "otkryt" ]; then
      echo "  КРАСНЫЙ: ядро поднялось на испорченном .srs — значит либо игнорирует файл, либо D зелёный просто так"
      bed=1
    else
      echo "  ожидаемое красное поведение ЯДРА зафиксировано ($ITOG_E): ядро реально читает комплект с диска — D что-то доказывает"
    fi
    pogasit "$PID_E"
  fi
fi

echo
echo "── итог: $([ $bed -eq 0 ] && echo ЗЕЛЁНЫЙ || echo КРАСНЫЙ) ──"
exit $bed
