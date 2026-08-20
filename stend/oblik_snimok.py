#!/usr/bin/env python3
"""Снимок облика приложения без Windows: поднимаем встроенный index.html,
подсовываем ему выдуманные ответы /api/* и фотографируем окно 420x660 —
ровно тот размер, что открывается у человека (cmd/kelevra/okno_windows.go).

Зачем: дизайн нельзя «проверить тестами», его надо УВИДЕТЬ. Смотреть на него
на живой машине Вовы — значит делать из него бета-тестера (его слова 20.08).

    python3 stend/oblik_snimok.py [папка-для-png]
"""
import http.server, json, socketserver, sys, threading
from pathlib import Path

KOREN = Path(__file__).resolve().parent.parent
OBLIK = KOREN / "internal" / "sluzhba" / "oblik"
VYHOD = Path(sys.argv[1]) if len(sys.argv) > 1 else KOREN / ".stend" / "oblik"

UZLY = {"gruppy": [{
    "imya": "Выбор узла", "sam": False, "seychas": "Нидерланды 2",
    "uzly": [
        {"imya": "Нидерланды 1", "zaderzhka": 78},
        {"imya": "Нидерланды 2", "zaderzhka": 64},
        {"imya": "Германия 1", "zaderzhka": 91},
        {"imya": "Финляндия 1", "beda": "нет ответа"},
    ]}]}

BAZA = {"versiya": "0.5.3", "kod_est": True, "sost": "stoit",
        "vniz_bayt": 0, "vverh_bayt": 0, "pid": 0,
        "imya": "Вова", "do_unix": 1789430400, "rezhim": "", "zametka": "",
        "mozhno_tun": False, "ruchnoy_proksi": False,
        "beda": "", "kachaem_yadro": False, "yadro_est": True}

SCENY = {
    "1_kod": dict(BAZA, kod_est=False),
    "2_otklyucheno": dict(BAZA, zametka="Ядро на месте, всё готово."),
    "3_podnimaem": dict(BAZA, sost="podnimaem", imya="Нидерланды 2"),
    "4_rabotaet": dict(BAZA, sost="rabotaet", pid="8124",
                       rezhim="proksi", vniz_bayt=418_365_440, vverh_bayt=21_495_808,
                       mozhno_tun=True, zametka="прокси-режим: в профиле нет туннеля"),
    "7_ruchnoy_proksi": dict(BAZA, sost="rabotaet", pid="8124", rezhim="proksi",
                             vniz_bayt=5_242_880, vverh_bayt=524_288, ruchnoy_proksi=True,
                             zametka="система не дала настроить прокси сама: "
                                     "пропишите в её настройках 127.0.0.1:2412"),
    "5_slomalos": dict(BAZA, sost="slomalos",
                       beda="ядро не ответило за 45 секунд: FATAL[0000] start service: "
                            "initialize inbound/tun[0]: configure tun interface: "
                            "permission denied"),
    "6_kachaem": dict(BAZA, kachaem_yadro=True, yadro_est=False),
    # Автозапуск живёт только на Windows, и облик прячет его ряд целиком по
    # avtozapusk_podderzhivaetsya. Стенд гоняется на Linux, поэтому без этих
    # сцен галочку не видит НИКТО — ни я, ни снимок: 20.08 она уехала в
    # релиз, ни разу не показавшись глазам. Поле подаём руками.
    "8_avtozapusk_vykl": dict(BAZA, zametka="Ядро на месте, всё готово.",
                              avtozapusk_podderzhivaetsya=True, avtozapusk_vklyuchen=False),
    "9_avtozapusk_vkl": dict(BAZA, zametka="Ядро на месте, всё готово.",
                             avtozapusk_podderzhivaetsya=True, avtozapusk_vklyuchen=True),
    "10_avtozapusk_ustarela": dict(BAZA, zametka="Ядро на месте, всё готово.",
                                   avtozapusk_podderzhivaetsya=True, avtozapusk_vklyuchen=True,
                                   avtozapusk_ustarela=True),
}

sostoyanie = {"tek": SCENY["2_otklyucheno"]}


class Ruchki(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *a, **kw):
        super().__init__(*a, directory=str(OBLIK), **kw)

    def _json(self, telo):
        b = json.dumps(telo).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(b)))
        self.end_headers()
        self.wfile.write(b)

    def do_GET(self):
        if self.path.startswith("/api/sostoyanie"):
            return self._json(sostoyanie["tek"])
        if self.path.startswith("/api/uzly"):
            return self._json(UZLY if sostoyanie["tek"]["sost"] == "rabotaet"
                              else {"gruppy": []})
        if self.path.startswith("/api/zhurnal"):
            self.send_response(200)
            self.send_header("Content-Type", "text/plain; charset=utf-8")
            self.end_headers()
            self.wfile.write(("2026/08/20 12:14:02 ядро запущено: pid 8124\n"
                              "2026/08/20 12:14:03 ядро ответило: связь работает\n"
                              "2026/08/20 12:31:44 останавливаю ядро\n"
                              "2026/08/20 12:31:44 системный прокси снят\n").encode())
            return
        return super().do_GET()

    def log_message(self, *a):
        pass


SHIRINA, VYSOTA = 420, 660

# JS-щуп: геометрия каждой видимой интерактивной штуки (кнопки, переключатели)
# плюс полная высота документа. getBoundingClientRect() меряет РЕАЛЬНОЕ место
# в окне — то же самое, что видит человек, открыв окно и ничего не тронув
# (scrollTop=0 — это и есть первый кадр, который на самом деле ловят люди).
IZMERENIE_JS = """() => {
  function skryt(el) {
    if (el.hidden) return true;
    const st = getComputedStyle(el);
    if (st.display === "none" || st.visibility === "hidden") return true;
    for (let p = el.parentElement; p; p = p.parentElement) {
      if (p.hidden) return true;
      const ps = getComputedStyle(p);
      if (ps.display === "none" || ps.visibility === "hidden") return true;
    }
    return false;
  }
  const shtuki = [...document.querySelectorAll("button, [role=switch]")]
    .filter((el) => !skryt(el))
    .map((el) => {
      const r = el.getBoundingClientRect();
      if (r.width === 0 && r.height === 0) return null;
      return {
        imya: (el.id || el.textContent || "").trim().replace(/\\s+/g, " ").slice(0, 70),
        top: r.top, bottom: r.bottom, left: r.left, right: r.right,
      };
    })
    .filter(Boolean);
  return {shtuki, scrollHeight: document.documentElement.scrollHeight};
}"""


def proverit_geometriyu(str_, imya_sceny):
    """Возвращает список бед (строк) для сцены: что уехало и на сколько px.

    Две беды под одним именем «уехал за границы»:
    1. штука целиком вне холста 420x660 — старый баг без прокрутки (об.
       662f757), когда страница просто росла вниз;
    2. штука ВНУТРИ холста, но под зафиксированной снизу панелью («.niz»,
       z-index:3) — новый баг: «.niz» ловит клики по всему своему
       прямоугольнику, так что перекрытая кнопка не видна и не тыкается,
       хотя формально «в окне». Мерим на первом кадре (scrollTop=0) —
       это и есть то, что видит человек, открыв окно и ничего не тронув.
    """
    dannye = str_.evaluate(IZMERENIE_JS)
    shtuki = dannye["shtuki"]
    bedy = []

    for sh in shtuki:
        if sh["left"] < 0:
            bedy.append(f'{imya_sceny}: «{sh["imya"]}» уехал за левый край '
                        f'на {-sh["left"]:.0f}px')
        if sh["right"] > SHIRINA:
            bedy.append(f'{imya_sceny}: «{sh["imya"]}» уехал за правый край '
                        f'на {sh["right"] - SHIRINA:.0f}px')
        if sh["top"] < 0:
            bedy.append(f'{imya_sceny}: «{sh["imya"]}» уехал за верхний край '
                        f'на {-sh["top"]:.0f}px')
        if sh["bottom"] > VYSOTA:
            bedy.append(f'{imya_sceny}: «{sh["imya"]}» уехал за нижний край '
                        f'на {sh["bottom"] - VYSOTA:.0f}px (bottom={sh["bottom"]:.0f})')

    # Перекрытие двух интерактивных штук: одна лежит поверх другой (обычно —
    # зафиксированная снизу панель наезжает на то, что прокручивается). Та,
    # что снизу по слоям, для человека не существует — не видна и не тыкается,
    # даже если формально её bottom меньше 660.
    for i in range(len(shtuki)):
        for j in range(i + 1, len(shtuki)):
            a, b = shtuki[i], shtuki[j]
            verh = max(a["top"], b["top"])
            niz_ = min(a["bottom"], b["bottom"])
            levo = max(a["left"], b["left"])
            pravo = min(a["right"], b["right"])
            if niz_ > verh and pravo > levo:
                bedy.append(f'{imya_sceny}: «{a["imya"]}» перекрыт «{b["imya"]}» '
                            f'на {niz_ - verh:.0f}px по высоте')

    if dannye["scrollHeight"] > VYSOTA:
        bedy.append(f'{imya_sceny}: документ выше окна на '
                    f'{dannye["scrollHeight"] - VYSOTA:.0f}px (scrollHeight={dannye["scrollHeight"]:.0f})')
    return bedy


def snyat():
    from playwright.sync_api import sync_playwright
    VYHOD.mkdir(parents=True, exist_ok=True)
    vse_bedy = []
    with socketserver.TCPServer(("127.0.0.1", 0), Ruchki) as srv:
        port = srv.server_address[1]
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        with sync_playwright() as p:
            br = p.chromium.launch()
            for imya, sost in SCENY.items():
                sostoyanie["tek"] = sost
                str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
                str_.goto(f"http://127.0.0.1:{port}/index.html")
                str_.wait_for_timeout(700)
                if imya == "5_slomalos":  # журнал раскрыт: его видно только так
                    str_.click("#knopka-zhurnal")
                    str_.wait_for_timeout(400)
                bedy = proverit_geometriyu(str_, imya)
                vse_bedy.extend(bedy)
                put = VYHOD / f"{imya}.png"
                str_.screenshot(path=str(put))
                znak = "🔴" if bedy else "🟢"
                print(f"  {znak} {put}")
                for b in bedy:
                    print(f"      {b}")
                str_.close()
            br.close()
        srv.shutdown()
    return vse_bedy


if __name__ == "__main__":
    print(f"облик: {OBLIK}")
    bedy = snyat()
    if bedy:
        print(f"\nКРАСНО: {len(bedy)} находок уезжают за границы окна {SHIRINA}x{VYSOTA}.")
        sys.exit(1)
    print("\nВсе сцены зелёные.")
