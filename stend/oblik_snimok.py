#!/usr/bin/env python3
"""Снимок облика приложения без Windows: поднимаем встроенный index.html,
подсовываем ему выдуманные ответы /api/* и фотографируем окно 420x660 —
ровно тот размер, что открывается у человека (cmd/kelevra/okno_windows.go).

Зачем: дизайн нельзя «проверить тестами», его надо УВИДЕТЬ. Смотреть на него
на живой машине Вовы — значит делать из него бета-тестера (его слова 20.08).

    python3 stend/oblik_snimok.py [папка-для-png]
"""
import http.server, json, os, socketserver, sys, threading
from pathlib import Path

KOREN = Path(__file__).resolve().parent.parent
# Папку облика можно подменить: так стенд гоняется против СТАРОЙ версии окна
# и доказывает, что щуп краснеет там, где беда была на самом деле.
OBLIK = Path(os.environ.get("KELEVRA_OBLIK") or (KOREN / "internal" / "sluzhba" / "oblik"))
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

# JS-щуп: до каждой ли видимой кнопки человек ДОБЕРЁТСЯ.
#
# Первая версия щупа (20.08) мерила только первый кадр и звала бедой всё, что
# ниже 660px. Внутри окна лежит своя прокрутка («.lenta», overflow-y:auto), и
# 10 из 10 её находок оказались обычной прокруткой — ложный красный на живом
# продукте. Поэтому мерим не «видно сразу», а «дотянуться можно»:
#   1. просим браузер докрутить контейнер до штуки — ровно то, что делает рукой
#      человек. Не докрутилось (штука всё равно за краем окна) — беда настоящая:
#      прокрутки нет или её не хватает;
#   2. смотрим elementFromPoint в центре штуки: если сверху лежит кто-то чужой
#      (зафиксированная снизу панель «.niz», модалка, наехавшая карточка) —
#      кнопка не тыкается, сколько её ни крути. Именно так ловится перекос
#      «padding-bottom у ленты — константа 98px, а высота панели зависит от
#      сцены»: панель выше константы — и последний ряд списка навсегда под ней.
DOSTUP_JS = """() => {
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
  function klichka(el) {
    if (!el) return "пусто";
    return el.id || (typeof el.className === "string" && el.className.trim())
           || el.tagName.toLowerCase();
  }
  const W = innerWidth, H = innerHeight;
  const bedy = [];
  const shtuki = [...document.querySelectorAll("button, [role=switch]")].filter((el) => !skryt(el));
  for (const el of shtuki) {
    let r = el.getBoundingClientRect();
    if (r.width === 0 && r.height === 0) continue;
    const imya = (el.id || el.textContent || "").trim().replace(/\\s+/g, " ").slice(0, 70);
    el.scrollIntoView({block: "center", inline: "center", behavior: "instant"});
    r = el.getBoundingClientRect();
    if (r.left < 0) bedy.push(`«${imya}» не достать: за левым краем на ${Math.round(-r.left)}px`);
    if (r.right > W) bedy.push(`«${imya}» не достать: за правым краем на ${Math.round(r.right - W)}px`);
    if (r.top < 0) bedy.push(`«${imya}» не достать: за верхним краем на ${Math.round(-r.top)}px`);
    if (r.bottom > H) bedy.push(`«${imya}» не достать: за нижним краем на ${Math.round(r.bottom - H)}px (прокрутка не спасает)`);
    const cx = Math.min(Math.max(r.left + r.width / 2, 0), W - 1);
    const cy = Math.min(Math.max(r.top + r.height / 2, 0), H - 1);
    const sverhu = document.elementFromPoint(cx, cy);
    if (!sverhu || (!el.contains(sverhu) && !sverhu.contains(el))) {
      bedy.push(`«${imya}» не тыкается: в его центре (${Math.round(cx)},${Math.round(cy)}) лежит «${klichka(sverhu)}»`);
    }
  }
  document.querySelectorAll("*").forEach((el) => { if (el.scrollTop) el.scrollTop = 0; });
  return bedy;
}"""


def proverit_geometriyu(str_, imya_sceny):
    """Список бед сцены: до чего человек не доберётся ни глазом, ни мышью."""
    return [f"{imya_sceny}: {b}" for b in str_.evaluate(DOSTUP_JS)]


# Порча для контроля: делаем нижнюю панель заведомо выше, чем зазор, который
# лента держит под неё (--nizhnyaya, константа). Это ровно та беда, ради
# которой щуп и живёт: панель переросла зазор — и последний ряд списка
# навсегда под ней, никакой прокруткой не достать.
PORCHA_CSS = ".niz { padding-bottom: 140px !important; }"


def kontrol_shchupa(br, port):
    """Щуп обязан покраснеть на испорченной странице. Промолчал — он мёртвый.

    Зелёный щуп ничего не значит сам по себе: 20.08 предыдущая версия этого
    же файла краснела 10 раз подряд на здоровом окне, а разбираться пришлось
    руками. Дешевле держать в стенде одну заведомо больную сцену.
    """
    sostoyanie["tek"] = SCENY["4_rabotaet"]
    str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
    str_.goto(f"http://127.0.0.1:{port}/index.html")
    str_.wait_for_timeout(700)
    str_.add_style_tag(content=PORCHA_CSS)
    str_.wait_for_timeout(200)
    bedy = proverit_geometriyu(str_, "контроль")
    str_.close()
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
                # Снимок ПЕРВЫМ: щуп крутит страницу, а глазам нужен первый
                # кадр — то, что человек видит, ничего не тронув.
                put = VYHOD / f"{imya}.png"
                str_.screenshot(path=str(put))
                bedy = proverit_geometriyu(str_, imya)
                vse_bedy.extend(bedy)
                znak = "🔴" if bedy else "🟢"
                print(f"  {znak} {put}")
                for b in bedy:
                    print(f"      {b}")
                str_.close()
            kontrol = kontrol_shchupa(br, port)
            br.close()
        srv.shutdown()
    return vse_bedy, kontrol


if __name__ == "__main__":
    print(f"облик: {OBLIK}")
    bedy, kontrol = snyat()
    if kontrol:
        print(f"\n  🧪 контроль: щуп видит порчу ({len(kontrol)} находок), например:")
        print(f"      {kontrol[0]}")
    else:
        print("\n🔴 ЩУП МЁРТВ: на заведомо испорченной странице он смолчал. "
              "Зелень остальных сцен ничего не доказывает.")
        sys.exit(2)
    if bedy:
        print(f"\nКРАСНО: {len(bedy)} кнопок не достать в окне {SHIRINA}x{VYSOTA}.")
        sys.exit(1)
    print("\nВсе сцены зелёные.")
