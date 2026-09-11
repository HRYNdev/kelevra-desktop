#!/usr/bin/env bash
# Регрессия: vypusk.sh не должен сам собирать и публиковать релиз, если в
# репозитории уже есть workflow, который выпускает по тегу app-v* сам (и,
# с 08.09, ждёт зелёной настоящей Windows перед этим). Иначе — гонка: тег ушёл,
# и одновременно скрипт создаёт СВОЙ релиз с линуксовой сборкой, не прошедшей
# тот гейт.
#
# Гоняем ОБЕ версии — сегодняшнюю ($KOREN/vypusk.sh) и версию ДО правки
# (вшита ниже целиком, как она была до этого коммита) — на одной и той же
# самодельной фикстуре (свой bare-репозиторий, свой .github/workflows/vypusk.yml
# с триггером на тег, свой крошечный go-модуль). git и curl подменены
# заглушками через PATH: push тега на github.com перехватывается безусловно
# (не доходит до сети, даже если бы токен-пустышка вдруг оказался настоящим),
# а curl отвечает по образцу вместо похода на api.github.com. НАСТОЯЩИЙ тег
# app-v* никогда не покидает эту песочницу.
#
#   stend/vypusk_ne_obhodit_gejt.sh
#
# Ожидание:
#   novyy  (сегодняшний код) — тег ушёл, POST .../releases НЕ вызван
#   staryy (код до правки)   — тег ушёл, И POST .../releases вызван — это и
#                              есть гонка, поэтому прогон staryy обязан быть
#                              КРАСНЫМ: если он вдруг зелёный, фикстура не
#                              воспроизводит беду и доверять этой регрессии нельзя.
set -eu
KOREN=$(cd "$(dirname "$0")/.." && pwd)
export PATH="$PATH:/usr/local/go/bin"
export GOCACHE=${GOCACHE:-${HOME:-/root}/.cache/go-build}

PLOSHCHADKA=$(mktemp -d)
trap 'rm -rf "$PLOSHCHADKA"' EXIT
FR="$PLOSHCHADKA/fikstura"
mkdir -p "$FR/zaglushki"

# ── заглушки git и curl ──────────────────────────────────────────────────
cat > "$FR/zaglushki/git" <<'EOF'
#!/usr/bin/env bash
# push с адресом на github.com — это push НАСТОЯЩЕГО тега в НАСТОЯЩИЙ
# репозиторий: сеть его не должна увидеть НИКОГДА, поэтому он не доходит до
# настоящего git, только логируется как «случившийся». Остальное идёт через
# настоящий git — origin у нас локальный bare-репозиторий рядом, не github.
if [ "$1" = "push" ]; then
  for a in "$@"; do
    case "$a" in
      *github.com*)
        echo "GIT $* [ПЕРЕХВАЧЕНО: push на github.com не выполнен]" >> "$FAKE_LOG/git.log"
        exit 0
        ;;
    esac
  done
fi
echo "GIT $*" >> "$FAKE_LOG/git.log"
exec "$REAL_GIT" "$@"
EOF

cat > "$FR/zaglushki/curl" <<'EOF'
#!/usr/bin/env bash
echo "CURL $*" >> "$FAKE_LOG/curl.log"
URL=""; OUT_FILE=""; prev=""
for a in "$@"; do
  case "$a" in http*) URL="$a" ;; esac
  [ "$prev" = "-o" ] && OUT_FILE="$a"
  prev="$a"
done
case "$URL" in
  *uploads.github.com*/assets*) printf '%s' '{"state":"uploaded","name":"Kelevra.exe","size":12345}' ;;
  *"/actions/runs?"*)           printf '%s' '{"workflow_runs":[{"id":4242,"path":".github/workflows/vypusk.yml"}]}' ;;
  */actions/runs/4242)          printf '%s' '{"status":"completed","conclusion":"success"}' ;;
  */releases/tags/*)            printf '%s' '{"message":"Not Found"}' ;;
  */releases/download/*)        [ -n "$OUT_FILE" ] && : > "$OUT_FILE" ;;
  */releases/999)               printf '%s' '{}' ;;
  */releases)                   printf '%s' '{"id":999}' ;;
  *)                            printf '%s' '{}' ;;
esac
exit 0
EOF
chmod +x "$FR/zaglushki/git" "$FR/zaglushki/curl"

# ── самодельная фикстура: bare origin + workflow с триггером на app-v* ───
git init -q --bare "$FR/origin.git"
git clone -q "$FR/origin.git" "$FR/nachalo"
(
  cd "$FR/nachalo"
  git checkout -q -b main
  mkdir -p internal/podpiska cmd/kelevra .github/workflows
  printf 'module github.com/HRYNdev/kelevra-desktop\n\ngo 1.21\n' > go.mod
  printf 'package podpiska\n\nvar Versiya = "0.1.0-rabota"\n' > internal/podpiska/podpiska.go
  cat > cmd/kelevra/main.go <<'GOEOF'
package main

import (
	"fmt"

	"github.com/HRYNdev/kelevra-desktop/internal/podpiska"
)

func main() { fmt.Println("запуск Kelevra " + podpiska.Versiya) }
GOEOF
  cat > .github/workflows/vypusk.yml <<'YMLEOF'
name: kelevra-vypusk
on:
  push:
    tags: ["app-v*"]
jobs:
  vypusk:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
YMLEOF
  git add -A
  git -c user.email=t@t -c user.name=t commit -q -m "фикстура регрессии"
  git push -q origin main
)
BAZA=$(git -C "$FR/origin.git" rev-parse main)

# ── версия vypusk.sh ДО этой правки, вшита целиком ────────────────────────
cat > "$FR/vypusk_staryy.sh" <<'STARYYEOF'
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

if ! grep -qa "$VERSIYA" "$VYHOD/Kelevra.exe"; then
  echo "✗ в собранном exe нет строки $VERSIYA: ldflags не сработали"; exit 1
fi
echo "   $(stat -c%s "$VYHOD/Kelevra.exe") байт, версия внутри найдена"

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

echo "✓ https://github.com/$REPO/releases/tag/$TEG"
STARYYEOF

cp "$KOREN/vypusk.sh" "$FR/vypusk_novyy.sh"
REAL_GIT=$(command -v git)

# ── прогон одной версии на чистой копии фикстуры ──────────────────────────
progon() {
  local kakoy="$1" rc=0
  local ploshchadka; ploshchadka=$(mktemp -d)
  export FAKE_LOG="$ploshchadka/log"; mkdir -p "$FAKE_LOG"
  git -C "$FR/origin.git" branch -f main "$BAZA" >/dev/null

  git clone -q "$FR/origin.git" "$ploshchadka/rabota"
  (
    cd "$ploshchadka/rabota"
    export REAL_GIT
    export PATH="$FR/zaglushki:$PATH"
    git checkout -q main
    cp "$FR/vypusk_$kakoy.sh" ./vypusk.sh
    chmod +x vypusk.sh
    git add vypusk.sh
    git -c user.email=t@t -c user.name=t commit -q -m "vypusk.sh $kakoy"
    git push -q origin main
    GITHUB_TOKEN=fake-token BEZ_PRIYOMKI=1 BEZ_WINE=1 TAYMAUT_CI=30 \
      ./vypusk.sh 9.9.9 "регрессия"
  ) > "$ploshchadka/vyhod.log" 2>&1 || rc=$?
  cat "$ploshchadka/vyhod.log" >&2

  local teg_pushnut=0 release_post=0
  grep -q "push.*app-v9.9.9" "$FAKE_LOG/git.log" 2>/dev/null && teg_pushnut=1
  grep -qE "CURL .*-X POST.*/releases[\"' ]" "$FAKE_LOG/curl.log" 2>/dev/null && release_post=1
  echo "   [$kakoy] тег запушен=$teg_pushnut, POST /releases вызван=$release_post, rc=$rc" >&2
  rm -rf "$ploshchadka"

  echo "$teg_pushnut $release_post"
}

echo "=== прогон: НОВЫЙ vypusk.sh (сегодняшний) ===" >&2
read -r TP_N RP_N <<< "$(progon novyy)"

echo >&2
echo "=== прогон: СТАРЫЙ vypusk.sh (до этой правки) ===" >&2
read -r TP_S RP_S <<< "$(progon staryy)"

echo
OK=1
if [ "$TP_N" = 1 ] && [ "$RP_N" = 0 ]; then
  echo "✓ НОВЫЙ код: тег ушёл, релиз сам не создал — отдал CI"
else
  echo "✗ НОВЫЙ код повёл себя не так, как ожидалось"
  OK=0
fi
if [ "$TP_S" = 1 ] && [ "$RP_S" = 1 ]; then
  echo "✓ СТАРЫЙ код (как и должен был) воспроизвёл гонку: тег ушёл И релиз создал сам"
else
  echo "✗ СТАРЫЙ код не воспроизвёл гонку — фикстура не годится, регрессии доверять нельзя"
  OK=0
fi

[ "$OK" = 1 ] && exit 0 || exit 1
