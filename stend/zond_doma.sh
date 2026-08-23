#!/usr/bin/env bash
# Стенд «зонд дома видит физического сеть, не туннель»: доказывает живьём
# (настоящая домашняя сеть этой машины, настоящий internal/avtorezhim —
# cmd/zamer_zond зовёт ровно те же DnsZond/SetevoyAdapter/Avtorezhim, что и
# продукт, самодельной имитации нет), что зонд DNS больше не привязан к
# системному резолверу и что слепота в TUN-режиме (avtorezhim.go,
# Nablyudeniye.ZondSlep) снимается ИМЕННО когда адрес физического адаптера
# известен и приватен.
#
# Диагноз 23.08. Системный резолвер в TUN-режиме перехвачен нашим же ядром
# (fakeip 198.18.0.0/15), поэтому dns_zond.go спрашивал не роутер, а себя —
# avtorezhim.go костылём (TunnelPodnyat) в этом режиме просто НАВСЕГДА
# выключал зонды: «дома» не определить никогда, пока поднят туннель. Живой
# замер этой машины (192.168.1.77) через домашний резолвер 192.168.1.192:
#   youtube.com   -> 198.18.3.10   (подмена)
#   discord.com   -> 198.18.9.93   (подмена)
#   rutracker.org -> 198.18.2.210  (подмена)
#   gosuslugi.ru  -> 213.59.x      (настоящий, контрольный домен)
# Лечение: DnsZond.AdresResolvera/LokalnyAdres (dns_zond.go) — зонд
# спрашивает конкретный резолвер напрямую, минуя системный путь; SetevoyAdapter
# (setevoy_adapter_*.go) узнаёт у ОС DNS физического адаптера; avtorezhim.go
# снимает ZondSlep, если этот адрес узнан и приватен.
#
# Сценарии:
#   A. зелёный — DnsZond с AdresResolvera=192.168.1.192:53 (домашний
#      резолвер) — признак дома (3 из 3, порог 2).
#   B. красный (ожидаемо) — тот же зонд, но AdresResolvera=1.1.1.1:53
#      (чужой публичный резолвер, реальные адреса без подмены) — признака
#      дома НЕТ (0 из 3). Контроль «зонд не залип на одном цвете» — A и B
#      обязаны разойтись.
#   C. SetevoyAdapter() на этой машине — что именно вернул (не-Windows ветка:
#      /etc/resolv.conf + net.Interfaces()).
#   D. Avtorezhim с TunnelPodnyat()==true (сценарий «полная защита», TUN
#      поднят): с настоящим SetevoyAdapter — наблюдение НЕ помечено
#      ZondSlep, вердикт после подтверждений — Doma; с адресом адаптера,
#      заведомо неизвестным (симуляция) — ZondSlep=true, вердикт не
#      сдвигается с VneDoma.
#
# Стенд красный (ненулевой код), если любой сценарий дал не то, что
# ожидалось. B красный — это ОЖИДАЕМОЕ поведение (контроль), а не провал
# стенда; провал стенда — это если B вдруг дал признак дома.
set -u
KOREN=$(cd "$(dirname "$0")/.." && pwd)
export PATH="$PATH:/usr/local/go/bin"
export HOME=${HOME:-/root}
export GOCACHE=${GOCACHE:-/tmp/gocache}

DOMASHNIY_RESOLVER="192.168.1.192:53"
CHUZHOY_RESOLVER="1.1.1.1:53"
STEND="$KOREN/.stend/zond_doma"
ZAMER="$STEND/zamer_zond"
bed=0

rm -rf "$STEND"
mkdir -p "$STEND"

echo "── сборка cmd/zamer_zond (гоняет настоящий internal/avtorezhim) ──"
if ! ( cd "$KOREN" && go build -o "$ZAMER" ./cmd/zamer_zond ) > "$STEND/build.log" 2>&1; then
  echo "  НЕ СОБРАЛСЯ:"; cat "$STEND/build.log"; exit 1
fi

echo
echo "── A. зелёный ожидается: DnsZond через домашний резолвер $DOMASHNIY_RESOLVER ──"
VYVOD_A=$("$ZAMER" -rezhim=dns -resolver="$DOMASHNIY_RESOLVER" -lokalny=192.168.1.77 -taimaut=6s 2>&1)
echo "  $VYVOD_A"
if ! echo "$VYVOD_A" | grep -q "doma=true"; then
  echo "  КРАСНЫЙ: домашний резолвер обязан дать признак дома (3 из 3 подменных) — не дал"
  bed=1
else
  echo "  зелёный: признак дома есть"
fi

echo
echo "── B. красный ожидается (контроль): тот же зонд через чужой резолвер $CHUZHOY_RESOLVER ──"
VYVOD_B=$("$ZAMER" -rezhim=dns -resolver="$CHUZHOY_RESOLVER" -lokalny=192.168.1.77 -taimaut=6s 2>&1)
echo "  $VYVOD_B"
if echo "$VYVOD_B" | grep -q "doma=true"; then
  echo "  КРАСНЫЙ: чужой публичный резолвер дал признак дома — зонд залип на одном цвете, замер недостоверен"
  bed=1
else
  echo "  ожидаемое красное поведение зафиксировано: чужой резолвер признака дома не даёт — контроль пройден"
fi

echo
echo "── C. SetevoyAdapter() на этой машине ──"
VYVOD_C=$("$ZAMER" -rezhim=adapter 2>&1)
echo "  $VYVOD_C"
if ! echo "$VYVOD_C" | grep -qE 'dns="[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+:[0-9]+"'; then
  echo "  КРАСНЫЙ: SetevoyAdapter не вернул непустой адрес резолвера"
  bed=1
else
  echo "  зелёный: адрес резолвера получен у ОС"
fi

echo
echo "── D. Avtorezhim, TunnelPodnyat()==true: настоящий SetevoyAdapter против симулированной неизвестности ──"
echo "  D1: адрес адаптера ИЗВЕСТЕН (настоящий SetevoyAdapter) — ожидаем ZondSlep=false, итог Doma"
echo "      (6 заходов, не 3: PryamoyZond реально ходит TCP до fake-IP терминатора роутера —"
echo "       живая сеть иногда теряет один пакет, задвижке нужно 3 ПОДРЯД, запас против шума)"
VYVOD_D1=$("$ZAMER" -rezhim=avtorezhim -zahodov=6 2>&1)
echo "$VYVOD_D1" | sed 's/^/    /'
if echo "$VYVOD_D1" | grep -q "ZondSlep=true"; then
  echo "  КРАСНЫЙ: с известным приватным адресом адаптера заход всё равно помечен слепым"
  bed=1
elif ! echo "$VYVOD_D1" | tail -1 | grep -q "tekushcheye=дома"; then
  echo "  КРАСНЫЙ: заход зрячий, но после подтверждений итог не Doma"
  bed=1
else
  echo "  зелёный: заход зрячий, итог Doma"
fi

echo "  D2: адрес адаптера НЕИЗВЕСТЕН (симуляция -slep) — ожидаем ZondSlep=true, итог остаётся VneDoma"
VYVOD_D2=$("$ZAMER" -rezhim=avtorezhim -slep 2>&1)
echo "$VYVOD_D2" | sed 's/^/    /'
if echo "$VYVOD_D2" | grep -q "ZondSlep=false"; then
  echo "  КРАСНЫЙ: без адреса адаптера заход не помечен слепым — костыль снят там, где ещё нужен"
  bed=1
elif ! echo "$VYVOD_D2" | tail -1 | grep -q "tekushcheye=вне дома"; then
  echo "  КРАСНЫЙ: слепой заход почему-то сдвинул вердикт"
  bed=1
else
  echo "  зелёный: заход слепой, вердикт держится VneDoma"
fi

echo
echo "── итог: $([ $bed -eq 0 ] && echo ЗЕЛЁНЫЙ || echo КРАСНЫЙ) ──"
exit $bed
