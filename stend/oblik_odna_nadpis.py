#!/usr/bin/env python3
"""Обстановку («дома»/«вне дома») говорит РОВНО ОДНО место, а не два.

Жалоба 28.08: под сообщением дважды написано, что вы дома и обход работает.
Причина: `#zametka` (большая заметка, её текст ставит zametkaAvtorezhima(s) —
index.html) и тихая строка `#zametka-avtorezhim` («сейчас: дома») рисовались
ОДНОВРЕМЕННО, когда авторежим включён, обстановка известна и защита не
поднята — обе несли один и тот же факт разными словами. Правка 28.08 добавила
тихой строке условие `&& !zametkaAvtorezhima(s)`: она молчит, если большая
заметка уже сказала то же самое, и говорит — если большая заметка молчит
(например обстановка «дома», а защита авторежимом уже поднята: sost==="rabotaet").

Стенд поднимает тот же встроенный сервер-заглушку, что и oblik_snimok.py, и
проверяет ОБЕ стороны правки на разных сценах:

  1_dvoynik   — «дома», авторежим включён, защита НЕ работает (sost="stoit"):
                #zametka несёт «Вы дома…», #zametka-avtorezhim ОБЯЗАН быть пуст.
                Порти условие (убери `&& !zametkaAvtorezhima(s)`) — эта сцена
                покраснеет: тихая строка снова задублирует обстановку.
  2_tihaya_zhiva — «дома», авторежим включён, но защита УЖЕ поднята
                (sost="rabotaet") — большая заметка про обстановку молчит
                (zametkaAvtorezhima возвращает null, см. index.html), и тихая
                строка обязана остаться живой: «сейчас: дома». Без этой сцены
                правка, убравшая строку СОВСЕМ, прошла бы стенд молча.

    python3 stend/oblik_odna_nadpis.py
"""
import socketserver, sys, threading
from pathlib import Path

KOREN = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(Path(__file__).resolve().parent))
from oblik_snimok import Ruchki, BAZA, ZAMETKI, sostoyanie, SHIRINA, VYSOTA  # noqa: E402

SCENY = {
    "1_dvoynik": dict(BAZA, sost="stoit", avtorezhim_vklyuchen=True,
                       avtorezhim_obstanovka="дома",
                       zametka=ZAMETKI["ZametkaBezTunnelya"]),
    "2_tihaya_zhiva": dict(BAZA, sost="rabotaet", pid="8124", rezhim="tunnel",
                            avtorezhim_vklyuchen=True, avtorezhim_obstanovka="дома",
                            zametka=ZAMETKI["ZametkaVes"]),
}

# Ждём: (кусок, который ОБЯЗАН быть в #zametka; текст, который ОБЯЗАН
# лежать в #zametka-avtorezhim — пустая строка значит «блок молчит»).
ZHDEM = {
    "1_dvoynik": ("Вы дома", ""),
    "2_tihaya_zhiva": (None, "сейчас: дома"),
}

TEKST_JS = """() => ({
  zametka: document.getElementById('zametka').textContent,
  avt: document.getElementById('zametka-avtorezhim').textContent,
})"""


def proverit(str_, imya_sceny):
    d = str_.evaluate(TEKST_JS)
    kusok_zametki, zhdem_avt = ZHDEM[imya_sceny]
    bedy = []
    if kusok_zametki and kusok_zametki not in d["zametka"]:
        bedy.append(f"{imya_sceny}: #zametka не сказал «{kusok_zametki}» "
                     f"(в блоке: «{d['zametka']}»)")
    if d["avt"] != zhdem_avt:
        opisanie = "пуст" if zhdem_avt == "" else f"«{zhdem_avt}»"
        bedy.append(f"{imya_sceny}: #zametka-avtorezhim ждали {opisanie}, "
                     f"а там «{d['avt']}» — обстановка сказана не там, где надо")
    if zhdem_avt and d["zametka"] and zhdem_avt.split(": ", 1)[-1] in d["zametka"]:
        # Дополнительная страховка: если обе строки НЕСУТ слово «дома» разом,
        # это и есть беда, ради которой правка появилась, — дубль обстановки.
        bedy.append(f"{imya_sceny}: обстановка названа И в #zametka, И в "
                     f"#zametka-avtorezhim разом — «{d['zametka']}» / «{d['avt']}»")
    return bedy


def zamerit():
    vse_bedy = []
    with socketserver.TCPServer(("127.0.0.1", 0), Ruchki) as srv:
        port = srv.server_address[1]
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        from playwright.sync_api import sync_playwright
        with sync_playwright() as p:
            br = p.chromium.launch()
            for imya_sceny, sost in SCENY.items():
                sostoyanie["tek"] = sost
                str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
                str_.goto(f"http://127.0.0.1:{port}/index.html")
                str_.wait_for_timeout(700)
                bedy = proverit(str_, imya_sceny)
                znak = "🔴" if bedy else "🟢"
                print(f"  {znak} {imya_sceny}")
                for b in bedy:
                    print(f"      {b}")
                vse_bedy.extend(bedy)
                str_.close()
            br.close()
        srv.shutdown()
    return vse_bedy


if __name__ == "__main__":
    bedy = zamerit()
    if bedy:
        print(f"\nКРАСНО: {len(bedy)} бед — обстановка «дома»/«вне дома» "
              "названа не там, где надо (дважды или ни разу).")
        sys.exit(1)
    print("\nОбстановку называет РОВНО одно место — заметка и тихая строка не спорят.")
