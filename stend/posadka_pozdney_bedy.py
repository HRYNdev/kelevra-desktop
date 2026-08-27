#!/usr/bin/env python3
"""Список узлов обязан пережить ОПОЗДАНИЕ блока над ним.

Беда (27.08, найдена в приёмке). `/api/uzly` и `/api/sostoyanie` — две
отдельные ручки, приезжают врозь, и порядок не гарантирован ничем. Высота
коробки списка подгонялась ОДНИМ замером в момент отрисовки списка. Когда
состояние опаздывало, список садился по экрану БЕЗ блока беды, а потом блок
беды вставал над ним и сдвигал список на 95px вниз. Выбранный узел уезжал под
панель вкладок целиком: `узел 643…687, лента 56…594`. Человек в этот момент не
видит, ЧТО у него выбрано, — ровно то, на что он жаловался 24.08 (снимки
11_beda_port.png/12_beda_seti.png). Само чинилось только следующим опросом,
через 2 секунды.

Почему щуп отдельный, а не сцена в oblik_geometriya.py. Тот забор смотрит
УСТОЯВШУЮСЯ вёрстку, и беда попадала в него случайно — красный ловился раз в
несколько прогонов и был неотличим от вранья прибора (23.08 такое уже
останавливало выпуск: proksi.sh). Здесь опоздание задаётся РУКОЙ, поэтому
красный либо есть всегда, либо его нет вовсе.

Контроль порчей (прогонял 27.08): убрать вызов `sleditZaVysotoyNadSpiskom()`
из index.html — щуп краснеет на сцене 5_slomalos теми же числами 643…687.

    python3 stend/posadka_pozdney_bedy.py
"""
import socketserver, sys, threading, time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from oblik_snimok import Ruchki, SCENY, sostoyanie, SHIRINA, VYSOTA  # noqa: E402
from oblik_geometriya import proverit_glavnyy_ekran  # noqa: E402

# Сцены, где над списком стоит блок беды: именно его опоздание двигает список.
# 5_slomalos — та, на которой беда поймана; остальные несут другой текст беды
# (другой высоты), то есть щуп меряет не один заученный пример.
SCENY_S_BEDOY = ["5_slomalos", "11_beda_port", "12_beda_seti"]
OPOZDANIE_MS = 500
# Отметки после загрузки. 700мс — окно, в котором беду поймала приёмка; 1500мс
# — то же окно уже после приезда опоздавшего состояния, ДО спасительного
# опроса на 2000мс. Смотреть только «через 3 секунды» бессмысленно: опрос
# чинит экран сам, и щуп зеленел бы поверх беды, которую человек уже увидел.
OTMETKI_MS = [700, 1500]


def proverit_scenu(br, port, imya_sceny):
    sostoyanie["tek"] = SCENY[imya_sceny]
    str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
    pervyy_otvet = [True]

    def priderzhat(route):
        # Придерживаем ТОЛЬКО первый ответ: дальше опросы идут своим ходом,
        # иначе щуп мерил бы вечно сломанную заглушку, а не живой порядок.
        if pervyy_otvet[0]:
            pervyy_otvet[0] = False
            time.sleep(OPOZDANIE_MS / 1000)
        route.continue_()

    str_.route("**/api/sostoyanie*", priderzhat)
    str_.goto(f"http://127.0.0.1:{port}/index.html")
    bedy = []
    proshlo = 0
    for otmetka in OTMETKI_MS:
        str_.wait_for_timeout(otmetka - proshlo)
        proshlo = otmetka
        for b in proverit_glavnyy_ekran(str_, imya_sceny):
            bedy.append(f"[{otmetka}мс после загрузки] {b}")
    str_.close()
    return bedy


def zamerit():
    vse_bedy = []
    with socketserver.TCPServer(("127.0.0.1", 0), Ruchki) as srv:
        port = srv.server_address[1]
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        from playwright.sync_api import sync_playwright
        with sync_playwright() as p:
            br = p.chromium.launch()
            for imya_sceny in SCENY_S_BEDOY:
                if imya_sceny not in SCENY:
                    vse_bedy.append(f"{imya_sceny}: сцены с таким именем больше нет — "
                                    f"щуп меряет пустоту, поправь список")
                    print(f"  🔴 {imya_sceny} — сцены нет")
                    continue
                bedy = proverit_scenu(br, port, imya_sceny)
                print(f"  {'🔴' if bedy else '🟢'} {imya_sceny} "
                      f"(состояние опоздало на {OPOZDANIE_MS}мс)")
                for b in bedy:
                    print(f"      {b}")
                vse_bedy.extend(bedy)
            br.close()
        srv.shutdown()
    return vse_bedy


if __name__ == "__main__":
    bedy = zamerit()
    if bedy:
        print(f"\nКРАСНО: {len(bedy)} бед. Опоздавший блок над списком сдвигает "
              f"список, и выбранный узел уходит с экрана.")
        sys.exit(1)
    print("\nСписок переживает опоздание блока над ним: выбранный узел виден "
          "на всех отметках.")
