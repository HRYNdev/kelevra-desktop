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


def snyat():
    from playwright.sync_api import sync_playwright
    VYHOD.mkdir(parents=True, exist_ok=True)
    with socketserver.TCPServer(("127.0.0.1", 0), Ruchki) as srv:
        port = srv.server_address[1]
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        with sync_playwright() as p:
            br = p.chromium.launch()
            for imya, sost in SCENY.items():
                sostoyanie["tek"] = sost
                str_ = br.new_page(viewport={"width": 420, "height": 660})
                str_.goto(f"http://127.0.0.1:{port}/index.html")
                str_.wait_for_timeout(700)
                if imya == "5_slomalos":  # журнал раскрыт: его видно только так
                    str_.click("#knopka-zhurnal")
                    str_.wait_for_timeout(400)
                put = VYHOD / f"{imya}.png"
                str_.screenshot(path=str(put))
                print(f"  {put}")
                str_.close()
            br.close()
        srv.shutdown()


if __name__ == "__main__":
    print(f"облик: {OBLIK}")
    snyat()
