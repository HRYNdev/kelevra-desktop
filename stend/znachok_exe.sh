#!/usr/bin/env bash
# Стенд «иконка в PE»: доказывает живьём (настоящая windows/amd64 сборка,
# не линуксовый двойник), что у собранного Kelevra.exe есть СВОЯ иконка на
# уровне PE-ресурсов, а не только байты для трея.
#
# Диагноз 23.08. cmd/kelevra/znachok.ico (15086 Б, 3 образа 48/32/16) до этой
# правки был встроен ТОЛЬКО как //go:embed в trey_windows.go — эти байты
# трей превращает в HICON вызовом CreateIconFromResourceEx уже во время
# работы (см. sobratZnachokTreya). В самом PE-файле ресурсов RT_ICON/
# RT_GROUP_ICON не было вовсе: в репозитории не было ни одного .syso/.rc,
# ни вызова goversioninfo/rsrc/windres. Значит Проводник, панель задач и
# Alt-Tab рисовали дефолтный значок Windows — про это и была жалоба.
# Лечение — cmd/kelevra/znachok_windows.syso, COFF-объект с тем же
# znachok.ico, собранный инструментом github.com/akavel/rsrc (см.
# «пересборка .syso» ниже); go build линкует любой *.syso из пакета main
# сам, без правок sборки.
#
# Что стенд ПРОВЕРЯЕТ: сам PE-файл, своим разбором (stend/pe_resursy.py —
# заголовки DOS/COFF/Optional/секции/дерево ресурсов на голом struct, без
# сторонних библиотек и без доверия к тому же rsrc, которым ресурсы туда
# положены). Зелёный, только если в .rsrc есть и RT_GROUP_ICON (14), и хотя
# бы один RT_ICON (3, считает и число образов).
#
# Чего стенд НЕ проверяет: как иконка реально рисуется в Проводнике/трее/
# Alt-Tab у живого человека — для этого нужна настоящая Windows с explorer.exe,
# которой здесь нет (см. границы stend/windows.sh и stend/trey.sh). Он же не
# трогает окно WebView2 (ustanovitZnachokOkna в okno_windows.go, WM_SETICON)
# — тот значок ставится во время исполнения, увидеть его можно только с
# живым HWND, до PE-ресурсов эта проверка не доезжает.
#
# Пересборка .syso после обновления znachok.ico (руками, инструмент — внешний,
# в go.mod продукта он не попадает, только `go run` из кеша):
#   export PATH=$PATH:/usr/local/go/bin HOME=/root
#   go run github.com/akavel/rsrc@latest -arch amd64 \
#     -ico cmd/kelevra/znachok.ico -o cmd/kelevra/znachok_windows.syso
#
# Проверка выключением (обязательна по договору со стендом): --lomat временно
# убирает .syso, собирает exe заново и показывает, что стенд краснеет (нет
# RT_GROUP_ICON/RT_ICON вовсе), затем возвращает файл на место (trap — даже
# при ошибке). Обычный прогон (без --lomat) проверяет только боевое
# состояние — .syso обязан лежать в репозитории.
set -u
KOREN=$(cd "$(dirname "$0")/.." && pwd)
export PATH="$PATH:/usr/local/go/bin"
export HOME=${HOME:-/root}
export GOCACHE=${GOCACHE:-/root/.cache/go-build}
command -v go >/dev/null 2>&1 || { echo "СТЕНД НЕ ЗАПУЩЕН: go нет в PATH (обычно /usr/local/go/bin; зови через bash -lc)"; exit 2; }

STEND="$KOREN/.stend_znachok"
mkdir -p "$STEND"
SYSO="$KOREN/cmd/kelevra/znachok_windows.syso"
EXE="$STEND/Kelevra.exe"

sobrat() {
  ( cd "$KOREN" && GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui" -o "$EXE" ./cmd/kelevra ) \
    > "$STEND/build.log" 2>&1
}

LOMAT=0
[ "${1:-}" = "--lomat" ] && LOMAT=1

if [ "$LOMAT" -eq 1 ]; then
  BEKAP="$STEND/znachok_windows.syso.bekap"
  if [ ! -f "$SYSO" ]; then
    echo "СТЕНД НЕ ЗАПУЩЕН: $SYSO отсутствует ещё до --lomat — ломать нечего"; exit 2
  fi
  mv "$SYSO" "$BEKAP"
  trap 'mv -f "$BEKAP" "$SYSO" 2>/dev/null' EXIT

  echo "── --lomat: .syso временно убран, ждём КРАСНЫЙ ──"
  if ! sobrat; then
    echo "СТЕНД НЕ ЗАПУЩЕН: go build без .syso не собрался"; cat "$STEND/build.log"; exit 2
  fi
  python3 "$KOREN/stend/pe_resursy.py" "$EXE"
  rc=$?
  if [ "$rc" -eq 0 ]; then
    echo "🔴 ПРОВАЛ ПРОВЕРКИ ВЫКЛЮЧЕНИЕМ: без .syso иконка в exe всё равно нашлась — стенд не отличает наличие правки от её отсутствия"
    exit 1
  fi
  echo "  ожидаемо красный без .syso (rc=$rc) — проверка выключением подтверждена"
  echo "── возвращаю .syso, жду ЗЕЛЁНЫЙ ──"
  mv -f "$BEKAP" "$SYSO"
  trap - EXIT
fi

if ! sobrat; then
  echo "СТЕНД НЕ ЗАПУЩЕН: go build не собрался"; cat "$STEND/build.log"; exit 2
fi

python3 "$KOREN/stend/pe_resursy.py" "$EXE"
rc_resursy=$?

# Ресурсы на месте — не значит, что цвет тот. stend/znachok_cvet.py разбирает
# самый крупный образ RT_ICON и проверяет оливу/бирюзу по HSV (см. диагноз
# в шапке того файла): значок может тихо остаться бирюзовым при живых
# RT_ICON/RT_GROUP_ICON, и rc_resursy этого не заметит.
python3 "$KOREN/stend/znachok_cvet.py" "$EXE"
rc_cvet=$?

[ "$rc_resursy" -eq 0 ] && [ "$rc_cvet" -eq 0 ]
exit $?
