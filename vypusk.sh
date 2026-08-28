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

api() { curl -sS -H "Authorization: Bearer $GITHUB_TOKEN" \
             -H "Accept: application/vnd.github+json" "$@"; }

if api "https://api.github.com/repos/$REPO/releases/tags/$TEG" | grep -q '"tag_name"'; then
  echo "✗ релиз $TEG уже есть"; exit 1
fi

echo "── тег и релиз $TEG"
git tag -f "$TEG" && git push -q "https://x-access-token:$GITHUB_TOKEN@github.com/$REPO.git" "$TEG"
TELO=$(python3 -c 'import json,sys; print(json.dumps({"tag_name":sys.argv[1],"name":"Kelevra "+sys.argv[2],"body":sys.argv[3]}))' \
       "$TEG" "$VERSIYA" "$OPISANIE")
ID=$(api -X POST "https://api.github.com/repos/$REPO/releases" -d "$TELO" \
     | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')

curl -sS -X POST -H "Authorization: Bearer $GITHUB_TOKEN" \
  -H "Content-Type: application/octet-stream" \
  --data-binary @"$VYHOD/Kelevra.exe" \
  "https://uploads.github.com/repos/$REPO/releases/$ID/assets?name=Kelevra.exe" \
  | python3 -c 'import json,sys; d=json.load(sys.stdin); print("   выложено:", d.get("name"), d.get("size"), "байт")'

echo "✓ https://github.com/$REPO/releases/tag/$TEG"
