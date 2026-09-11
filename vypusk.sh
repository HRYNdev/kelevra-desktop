#!/usr/bin/env bash
# Выпуск версии: приёмка → сборка → тег → релиз с Kelevra.exe.
#
# Зачем скриптом. Kelevra обновляется у человека САМА, из последнего релиза
# этого репозитория. Значит «сделать релиз» — это не публикация файла, а правка
# прямо на его машине, и делать её руками, по памяти, нельзя: забытый шаг
# приёмки уезжает к нему целиком. Поэтому приёмка тут не «рекомендуется», а
# стоит первой и её нельзя пропустить иначе как явным BEZ_PRIYOMKI=1.
#
#   GITHUB_TOKEN=… ./vypusk.sh 0.6.1 "что нового, строкой"
set -eu
VERSIYA=${1:?нужна версия, например 0.6.1}
OPISANIE=${2:-}
KOREN=$(cd "$(dirname "$0")" && pwd)
REPO=HRYNdev/kelevra-desktop
TEG="app-v$VERSIYA"
export PATH="$PATH:/usr/local/go/bin"
export HOME=${HOME:-/root}
export GOCACHE=${GOCACHE:-$HOME/.cache/go-build}
cd "$KOREN"

: "${GITHUB_TOKEN:?нет GITHUB_TOKEN}"

if [ -n "$(git status --porcelain)" ]; then
  echo "✗ в дереве есть неучтённые правки — выпускать нечего или выпустится не то"
  git status --short
  exit 1
fi
if [ "$(git rev-parse --abbrev-ref HEAD)" != "main" ]; then
  echo "✗ выпуск только с main"; exit 1
fi
git fetch -q origin main
if [ "$(git rev-parse HEAD)" != "$(git rev-parse origin/main)" ]; then
  echo "✗ main разъехался с origin/main: сперва подтяни"; exit 1
fi

# Сироты прошлого выпуска. Заход, из которого идёт выпуск, может умереть в любую
# минуту (обрыв, потолок подписки) — и тогда стенды остаются брошенными процессами
# под wine. Следующий выпуск они валят как «площадка нечистая»: стенд ждёт ровно
# один Kelevra.exe, видит два и краснеет. 28.08 ровно так: 29🟢/2🔴, и оба красных
# были от вчерашних сирот, а не от продукта — перегон на чистой площадке дал rc=0
# обоим. Бьём по pid, а не шаблоном по дереву: шаблон видит и свою же оболочку.
siroty=$(pgrep -f 'Kelevra\.exe' 2>/dev/null | grep -vx -e "$$" -e "$PPID" || true)
if [ -n "$siroty" ]; then
  echo "── сироты прошлого выпуска (Kelevra.exe): $(echo "$siroty" | tr '\n' ' ') — убираю"
  # shellcheck disable=SC2086
  kill $siroty 2>/dev/null || true
  for _ in 1 2 3 4 5; do
    sleep 1
    ostalis=$(pgrep -f 'Kelevra\.exe' 2>/dev/null | grep -vx -e "$$" -e "$PPID" || true)
    [ -z "$ostalis" ] && break
  done
  # shellcheck disable=SC2086
  [ -n "${ostalis:-}" ] && kill -9 $ostalis 2>/dev/null || true
  echo "── площадка очищена"
fi

if [ "${BEZ_PRIYOMKI:-0}" = "1" ]; then
  echo "⚠ приёмка ПРОПУЩЕНА по BEZ_PRIYOMKI=1 — это уедет на машину человека как есть"
else
  echo "── приёмка перед выпуском"
  PRIYOMKA_LOG=$(mktemp)
  set +e
  bash stend/vse.sh 2>&1 | tee "$PRIYOMKA_LOG"
  priyomka_rc=${PIPESTATUS[0]}
  set -e
  if [ "$priyomka_rc" -ne 0 ]; then
    # rc=7 у стенда (⚫ ПРИБОР МЁРТВ, stend/obshchee.sh) значит: wine сегодня
    # не смог запустить exe, продукт вообще не проверялся — это не брак
    # продукта, и выпускать вслепую нельзя так же, как и при красном.
    if grep -q "ПРИБОР МЁРТВ" "$PRIYOMKA_LOG"; then
      echo "✗ выпуск остановлен: стенд не смог проверить (wine мёртв), это не брак продукта"
    fi
    rm -f "$PRIYOMKA_LOG"
    exit 1
  fi
  rm -f "$PRIYOMKA_LOG"
fi

api() { curl -sS -H "Authorization: Bearer $GITHUB_TOKEN" \
             -H "Accept: application/vnd.github+json" "$@"; }

# Запись «этот тег — мой выпуск» в telo/dannye/vypusk_sverki.jsonl (см. doehalo).
# Общая для ОБОИХ путей ниже: и локальной сборки, и делегата в CI — CI тоже
# сам скачивает Kelevra.exe ради sha256-сверки (см. vypusk.yml, шаг «скачиваю
# как посторонний»), просто на чужой машине без доступа к telo, поэтому запись
# всё равно делает этот скрипт, он же и позвал CI. Каталог telo — моё тело, у
# постороннего форка его нет: тогда молча пропускаю запись, выпуск не падает.
zapisat_v_sverku() {
  local teg="$1" dannye_telo="/opt/jarvis-goal/telo/dannye"
  [ -d "$dannye_telo" ] || return 0
  (
    SCHET=$(api "https://api.github.com/repos/$REPO/releases/tags/$teg" \
      | python3 -c 'import json,sys
d=json.load(sys.stdin)
a=[x for x in d.get("assets",[]) if x["name"]=="Kelevra.exe"]
print(a[0]["download_count"] if a else 0)' 2>/dev/null)
    KOGDA=$(python3 -c 'from datetime import datetime,timezone; print(datetime.now(timezone.utc).isoformat())')
    python3 -c 'import json,sys
zap = {"teg": sys.argv[1], "kogda": sys.argv[2], "skachal_sam": 1,
       "schet_posle_sverki": int(sys.argv[3] or 0)}
with open(sys.argv[4], "a", encoding="utf-8") as f:
    f.write(json.dumps(zap, ensure_ascii=False) + "\n")' \
      "$teg" "$KOGDA" "$SCHET" "$dannye_telo/vypusk_sverki.jsonl"
  ) || echo "   (не записал сверку в vypusk_sverki.jsonl — выпуск это не останавливает)"
}

if api "https://api.github.com/repos/$REPO/releases/tags/$TEG" | grep -q '"tag_name"'; then
  echo "✗ релиз $TEG уже есть"; exit 1
fi

# С 01.09 тег app-v* сам по себе — событие: .github/workflows/vypusk.yml
# собирает и публикует релиз, а с 08.09 ещё и ждёт зелёной проверки на
# настоящей Windows перед этим. Если скрипт по старой памяти соберёт и
# опубликует релиз сам, получится гонка за один тег: либо два релиза, либо
# к человеку уезжает линуксовая сборка, которая тот гейт не проходила.
# Поэтому смотрим на факт — есть ли в репозитории такой workflow — а не на
# захардкоженное имя файла, и если есть, дальше работает CI, а не мы.
CI_VYPUSK_FAYL=$(python3 - "$KOREN" "$TEG" <<'PY' || true
import fnmatch, glob, os, sys
try:
    import yaml
except ImportError:
    sys.exit(1)
koren, teg = sys.argv[1], sys.argv[2]
shablony = glob.glob(os.path.join(koren, ".github/workflows/*.yml")) \
    + glob.glob(os.path.join(koren, ".github/workflows/*.yaml"))
for put in shablony:
    try:
        with open(put, encoding="utf-8") as f:
            dannye = yaml.safe_load(f) or {}
    except Exception:
        continue
    # YAML 1.1 читает голый `on:` как булев True — берём оба варианта.
    on = dannye.get("on", dannye.get(True))
    if not isinstance(on, dict):
        continue
    push = on.get("push")
    if not isinstance(push, dict):
        continue
    for teg_shablon in (push.get("tags") or []):
        if fnmatch.fnmatch(teg, teg_shablon):
            print(put)
            sys.exit(0)
sys.exit(1)
PY
)

if [ -n "$CI_VYPUSK_FAYL" ]; then
  echo "── тег $TEG уходит в CI ($CI_VYPUSK_FAYL) — сборку и релиз делает он"
  git tag -f "$TEG" && git push -q "https://x-access-token:$GITHUB_TOKEN@github.com/$REPO.git" "$TEG"

  SHA_TEGA=$(git rev-parse "$TEG^{commit}")
  CI_PUT_OTN=".github/workflows/$(basename "$CI_VYPUSK_FAYL")"
  TAYMAUT_CI=${TAYMAUT_CI:-1800}
  NACHALO=$(date +%s)
  echo "── жду, пока CI найдёт прогон по $SHA_TEGA (таймаут ${TAYMAUT_CI}с)"
  RUN_ID=""
  while [ -z "$RUN_ID" ]; do
    RUN_ID=$(api "https://api.github.com/repos/$REPO/actions/runs?head_sha=$SHA_TEGA&per_page=20" \
      | python3 -c "
import json, sys
d = json.load(sys.stdin)
for r in d.get('workflow_runs', []):
    if r.get('path') == '$CI_PUT_OTN':
        print(r['id']); break
" 2>/dev/null || true)
    if [ -n "$RUN_ID" ]; then break; fi
    if [ $(( $(date +%s) - NACHALO )) -gt "$TAYMAUT_CI" ]; then
      echo "✗ CI не начал прогон по тегу $TEG за ${TAYMAUT_CI}с — к человеку ничего не уехало"
      exit 1
    fi
    sleep 10
  done
  echo "   прогон найден: run $RUN_ID"

  ZAKLYUCHENIE=""
  while [ -z "$ZAKLYUCHENIE" ]; do
    OTVET=$(api "https://api.github.com/repos/$REPO/actions/runs/$RUN_ID")
    STATUS=$(echo "$OTVET" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("status",""))')
    if [ "$STATUS" = "completed" ]; then
      ZAKLYUCHENIE=$(echo "$OTVET" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("conclusion",""))')
      break
    fi
    if [ $(( $(date +%s) - NACHALO )) -gt "$TAYMAUT_CI" ]; then
      echo "✗ CI не закончил прогон по тегу $TEG за ${TAYMAUT_CI}с (run $RUN_ID висит) — к человеку ничего не уехало"
      exit 1
    fi
    sleep 15
  done

  if [ "$ZAKLYUCHENIE" = "success" ]; then
    echo "✓ CI зелёный: https://github.com/$REPO/releases/tag/$TEG"
    zapisat_v_sverku "$TEG"
    exit 0
  else
    echo "✗ CI закончился как «$ZAKLYUCHENIE» на $TEG — к человеку ничего не уехало: https://github.com/$REPO/actions/runs/$RUN_ID"
    exit 1
  fi
fi

echo "── сборка $VERSIYA"
VYHOD=$(mktemp -d)
trap 'rm -rf "$VYHOD"' EXIT
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build \
  -ldflags "-H=windowsgui -s -w -X github.com/HRYNdev/kelevra-desktop/internal/podpiska.Versiya=$VERSIYA" \
  -o "$VYHOD/Kelevra.exe" ./cmd/kelevra

# Версия обязана быть ВНУТРИ файла: пустой -X молча оставляет «0.1.0-rabota»,
# и тогда обновление у человека не сработает — он останется на старой навсегда.
if ! grep -qa "$VERSIYA" "$VYHOD/Kelevra.exe"; then
  echo "✗ в собранном exe нет строки $VERSIYA: ldflags не сработали"; exit 1
fi
echo "   $(stat -c%s "$VYHOD/Kelevra.exe") байт, версия внутри найдена"

# Стенды собирают СВОИ бинарники, без -s -w. Значит ровно та сборка, что
# уезжает человеку, не гонялась нигде: 20.08 она оказалась на 4 МБ легче
# релизной и разница объяснилась только замером. Гоняем именно её.
if [ "${BEZ_WINE:-0}" = "1" ]; then
  echo "⚠ выпускаемый exe НЕ запущен: BEZ_WINE=1"
else
  echo "── старт выпускаемого exe под wine"
  export WINEPREFIX=${WINEPREFIX:-$KOREN/.wine} WINEDEBUG=${WINEDEBUG:--all}
  LOG=$(mktemp)
  KELEVRA_BEZ_OKNA=1 timeout 40 "${WINE:-/usr/lib/wine/wine64}" \
    "$VYHOD/Kelevra.exe" --sluzhba >"$LOG" 2>&1 &
  PID=$!
  for _ in $(seq 1 25); do grep -q "служба слушает" "$LOG" && break; sleep 1; done
  kill "$PID" 2>/dev/null || true
  if ! grep -q "запуск Kelevra $VERSIYA" "$LOG"; then
    echo "✗ выпускаемый exe не назвался версией $VERSIYA:"; head -6 "$LOG"; exit 1
  fi
  if ! grep -q "служба слушает" "$LOG"; then
    echo "✗ выпускаемый exe не поднял службу:"; head -12 "$LOG"; exit 1
  fi
  echo "   стартовал, назвался $VERSIYA, служба поднялась"
  rm -f "$LOG"
fi

echo "── тег и релиз $TEG"
git tag -f "$TEG" && git push -q "https://x-access-token:$GITHUB_TOKEN@github.com/$REPO.git" "$TEG"

# ЧЕРНОВИК, а не сразу публикация. Обновление у человека берёт ПОСЛЕДНИЙ релиз и
# качает из него Kelevra.exe. Значит опубликованный релиз без файла — это не
# «полвыпуска», а поломка прямо у него: 28.08 выгрузка файла отдала пустой ответ,
# и на GitHub полминуты висела версия 0.6.33 вообще без .exe. Публикуем только
# после того, как файл лёг и виден снаружи.
TELO=$(python3 -c 'import json,sys; print(json.dumps({"tag_name":sys.argv[1],"name":"Kelevra "+sys.argv[2],"body":sys.argv[3],"draft":True}))' \
       "$TEG" "$VERSIYA" "$OPISANIE")
ID=$(api -X POST "https://api.github.com/repos/$REPO/releases" -d "$TELO" \
     | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')

SHA_MOY=$(sha256sum "$VYHOD/Kelevra.exe" | cut -d' ' -f1)
for popytka in 1 2 3; do
  OTVET=$(curl -sS -X POST -H "Authorization: Bearer $GITHUB_TOKEN" \
    -H "Content-Type: application/octet-stream" \
    --data-binary @"$VYHOD/Kelevra.exe" \
    "https://uploads.github.com/repos/$REPO/releases/$ID/assets?name=Kelevra.exe" || true)
  if echo "$OTVET" | grep -q '"state":"uploaded"'; then break; fi
  echo "   ✗ выгрузка $popytka/3 не удалась: $(echo "$OTVET" | head -c 200)"
  # недовыгруженный огрызок мешает повтору тем же именем — снимаем
  api "https://api.github.com/repos/$REPO/releases/$ID/assets" \
    | python3 -c 'import json,sys; [print(a["id"]) for a in json.load(sys.stdin) if a["name"]=="Kelevra.exe"]' \
    | while read -r aid; do api -X DELETE "https://api.github.com/repos/$REPO/releases/assets/$aid" >/dev/null; done
  if [ "$popytka" = 3 ]; then
    echo "✗ файл не выложился — релиз остаётся ЧЕРНОВИКОМ, к человеку ничего не уехало"; exit 1
  fi
  sleep 3
done
echo "   выложено: $(echo "$OTVET" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["name"], d["size"], "байт")')"

echo "── публикация после проверки, что файл виден снаружи"
api -X PATCH "https://api.github.com/repos/$REPO/releases/$ID" -d '{"draft":false}' >/dev/null
SKACHANO=$(mktemp)
curl -sSL -o "$SKACHANO" "https://github.com/$REPO/releases/download/$TEG/Kelevra.exe" || true
SHA_TAM=$(sha256sum "$SKACHANO" | cut -d' ' -f1); rm -f "$SKACHANO"
if [ "$SHA_MOY" != "$SHA_TAM" ]; then
  echo "✗ скачанное снаружи не совпало со сборкой ($SHA_MOY против $SHA_TAM)"; exit 1
fi
echo "   скачал как посторонний: sha256 совпал со сборкой"

# Эта же сверка сама создаёт +1 в download_count ассета — счётчик не отличает
# меня от постороннего. Пишу это скачивание СЕБЕ в зачёт, чтобы потом можно
# было вычесть его из «скачано», а не путать со спросом (см. телесный навык
# doehalo).
zapisat_v_sverku "$TEG"

echo "✓ https://github.com/$REPO/releases/tag/$TEG"
