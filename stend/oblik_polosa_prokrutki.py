#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Стенд: полоса прокрутки не должна быть родной полосой Chromium.

Зачем он появился. хозяин про окно, дословно (30.08): «эта *** полосочка в
приложении *** ***(которая отвечает за прокрут, выглядит криво)». Снимок
headed-chromium (3_crop_top.png) показал классику: у модалки .shtorka не было
НИ ОДНОГО правила ::-webkit-scrollbar — Chromium рисовал свою дефолтную,
светлую (~#f0f0f0 трек, ~#a8a8a8 ползунок), ~15px, с треугольными кнопками-
стрелками на концах, поперёк тёмного интерфейса.

Почему это не ловит oblik_snimok.py. Тот щуп смотрит headless — а headless
Chromium нативную полосу прокрутки не рисует ВООБЩЕ (её пикселей нет в
кадре), поэтому даже дырявый CSS там выглядел бы одинаково чисто что до, что
после правки. Этот щуп поэтому идёт headed под Xvfb, с явным
--disable-features=OverlayScrollbar (без него в headed-режиме современный
Chromium иногда включает overlay-полосу, которая не занимает места и не
ловится по геометрии рядом с контентом).

Что делает. Для каждого из четырёх скролл-контейнеров (.lenta, #tab-set,
.shtorka, #zhurnal) намеренно переполняет содержимое, ждёт кадр и снимает
узкую колонку у правого края контейнера (~16px) — ровно там, где сидит
полоса. Судит ПО ПИКСЕЛЯМ: тёмная палитра облика (--fon/--fon2/--gran/
--gran2 — ярче #3a3f37, то есть максимум ~56 по каналу) не имеет права дать в
этой колонке ни одного пикселя ярче ПОРОГ_СВЕТЛЫЙ (дефолтный трек/ползунок
Chromium — 168-252). Отдельно считает верхние/нижние ~16px колонки — там же
сидят дефолтные кнопки-стрелки, если их не сняли явным display:none.

Запуск:
  python3 stend/oblik_polosa_prokrutki.py            (rc=0 зелёный, rc=1 красный)
  python3 stend/oblik_polosa_prokrutki.py --kontrol   (обязан покраснеть —
                                                        временно снимает
                                                        общий блок правил)
"""
import io
import os
import re
import shutil
import socketserver
import subprocess
import sys
import threading
import time
from pathlib import Path

KOREN = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(KOREN / "stend"))
import oblik_snimok as osn  # переиспользуем сервер /api/* и index.html облика

from PIL import Image

SHIRINA, VYSOTA = osn.SHIRINA, osn.VYSOTA

# Между «наше» (--gran #2b2f29 ~44, --gran2 #3a3f37 ~56 по максимуму канала)
# и «чужое» (дефолтный трек Chromium ~#f0f0f0 ~240, дефолтный ползунок
# ~#a8a8a8 ~168) — пропасть, порог 100 лежит строго посередине с запасом.
POROG_SVETLYY = 100
# CSS-ширина полосы — 8px (см. index.html). Захват чуть шире (10px) даёт
# запас на сглаживание САМОЙ полосы, но не настолько широкий, чтобы зацепить
# соседний контент: 16px однажды поймал акцентный цвет обычного элемента
# списка в паре пикселей ОТ полосы, а не саму полосу (ложный красный).
SHIRINA_POLOSY = 10
KRAY_STRELKI = 16     # зона у верхнего/нижнего края колонки, где живут кнопки

# Сентинелы вокруг общего блока правил ::-webkit-scrollbar в index.html —
# см. сам файл. Регексом по ним, не построчным поиском слова "scrollbar":
# построчный вариант оставлял бы висячие "display:none; }" без селектора,
# то есть отдельную, свою собственную порчу CSS, а не ту, что была ДО правки.
SENTINEL_NACHALO = "ЩУП-ПОЛОСА-ПРОКРУТКИ: НАЧАЛО"
SENTINEL_KONEC = "ЩУП-ПОЛОСА-ПРОКРУТКИ: КОНЕЦ"
BLOK_RE = re.compile(
    r"/\*[^*]*" + re.escape(SENTINEL_NACHALO) + r".*?" + re.escape(SENTINEL_KONEC) + r"\s*\*/",
    re.DOTALL,
)

# Контейнеры и то, как до каждого добраться и чем его переполнить — селектор
# для переполнения не всегда совпадает с селектором в CSS: у #zhurnal сам
# <pre> не виден до клика по «Журнал работы».
KONTEYNERY = [
    # .lenta — но её СОБСТВЕННЫЙ скролл виден только на вкладке «Настройки»:
    # на «Сети» переполнение уходит во вложенный #tab-set (см. комментарий
    # у #tab-set в index.html — тот же капкан, что уже один раз ловил
    # oblik_obrezka_kadra.py).
    {"imya": ".lenta", "selector": "#lenta",
     "podgotovka": lambda p: p.click("#vkladka-nastroyki")},
    {"imya": "#tab-set", "selector": "#tab-set", "podgotovka": None},
    {"imya": ".shtorka", "selector": "#shtorka",
     "podgotovka": lambda p: p.click("#karta-vyhod")},
    {"imya": "#zhurnal", "selector": "#zhurnal",
     "podgotovka": lambda p: (p.click("#vkladka-nastroyki"), p.click("#knopka-zhurnal"))},
]

PEREPOLNIT_JS = """(sel) => {
  const el = document.querySelector(sel);
  if (!el) return false;
  let i = 0;
  while (el.scrollHeight <= el.clientHeight + 60 && i < 300) {
    const d = document.createElement('div');
    d.textContent = 'щуп-заполнитель строки ' + i;
    d.style.height = '22px';
    el.appendChild(d);
    i++;
  }
  return el.scrollHeight > el.clientHeight + 10;
}"""

RECT_JS = """(sel) => {
  const el = document.querySelector(sel);
  if (!el) return null;
  const r = el.getBoundingClientRect();
  return {top: r.top, right: r.right, bottom: r.bottom, left: r.left};
}"""


def zapustit_xvfb():
    """headless Chromium не рисует нативную полосу прокрутки вовсе — щупу
    нужен настоящий дисплей. Тот же приём, что у stend/trey.sh: используем
    чужой Xvfb, если он уже поднят на :97, свой — если нет."""
    display = os.environ.get("DISPLAY") or ":97"
    proc = None
    zapushchen = subprocess.run(
        ["xdpyinfo", "-display", display],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    ).returncode == 0
    if not zapushchen:
        proc = subprocess.Popen(
            ["Xvfb", display, "-screen", "0", "1280x800x24"],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        )
        time.sleep(2)
    os.environ["DISPLAY"] = display
    return proc


def izmerit_polosu(png_bytes, rect):
    """Считает светлые пиксели в колонке у правого края контейнера. Отдаёт
    (всего_светлых, светлых_сверху, светлых_снизу, ширина, высота_колонки)."""
    img = Image.open(io.BytesIO(png_bytes)).convert("RGB")
    left = max(0, int(round(rect["right"])) - SHIRINA_POLOSY)
    right = min(img.width, int(round(rect["right"])))
    top = max(0, int(round(rect["top"])))
    bottom = min(img.height, int(round(rect["bottom"])))
    if right <= left or bottom <= top:
        return 0, 0, 0, 0, 0
    polosa = img.crop((left, top, right, bottom))
    px = polosa.load()
    w, h = polosa.size

    def schitat(y0, y1):
        n = 0
        for y in range(max(0, y0), min(h, y1)):
            for x in range(w):
                if max(px[x, y]) >= POROG_SVETLYY:
                    n += 1
        return n

    kray = min(KRAY_STRELKI, h // 2) if h else 0
    vsego = schitat(0, h)
    verh = schitat(0, kray)
    niz = schitat(h - kray, h)
    return vsego, verh, niz, w, h


def proverit_odin_konteyner(br, port, kont, papka_snimkov):
    """Открывает свежую страницу, готовит и переполняет нужный контейнер,
    снимает кадр, возвращает (imya, bedy: list[str])."""
    imya = kont["imya"]
    bedy = []
    page = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
    try:
        page.goto(f"http://127.0.0.1:{port}/index.html")
        page.wait_for_timeout(400)
        if kont["podgotovka"]:
            kont["podgotovka"](page)
            page.wait_for_timeout(300)
        est = page.evaluate(PEREPOLNIT_JS, kont["selector"])
        if not est:
            bedy.append(f"{imya}: переполнить контейнер {kont['selector']} не удалось "
                        "— щуп слеп, а не облик чист")
            return imya, bedy
        page.wait_for_timeout(250)
        rect = page.evaluate(RECT_JS, kont["selector"])
        if rect is None:
            bedy.append(f"{imya}: элемент {kont['selector']} не найден на странице")
            return imya, bedy
        png = page.screenshot()
        (papka_snimkov / f"{imya.strip('.#')}_polosa.png").write_bytes(png)
        vsego, verh, niz, w, h = izmerit_polosu(png, rect)
        print(f"  {imya}: колонка {w}x{h}px у правого края, порог яркости "
              f"{POROG_SVETLYY} — светлых пикселей всего {vsego}, "
              f"сверху(≤{KRAY_STRELKI}px) {verh}, снизу(≤{KRAY_STRELKI}px) {niz}")
        if vsego > 0:
            bedy.append(f"{imya}: в полосе прокрутки {vsego} светлых пикселей "
                        f"(порог {POROG_SVETLYY}) — это не наш тёмный ползунок/трек, "
                        f"а дефолтная полоса Chromium (или её кнопка-стрелка)")
    finally:
        page.close()
    return imya, bedy


def progon(oblik_dir, papka_snimkov):
    """Полный прогон всех четырёх контейнеров против index.html из oblik_dir.
    Возвращает список бед (пустой — зелено)."""
    osn.OBLIK = Path(oblik_dir)
    osn.sostoyanie["tek"] = osn.SCENY["4_rabotaet"]
    vse_bedy = []
    papka_snimkov.mkdir(parents=True, exist_ok=True)
    with socketserver.TCPServer(("127.0.0.1", 0), osn.Ruchki) as srv:
        port = srv.server_address[1]
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        from playwright.sync_api import sync_playwright
        with sync_playwright() as p:
            br = p.chromium.launch(
                headless=False,
                args=["--disable-features=OverlayScrollbar", "--no-sandbox",
                      "--disable-dev-shm-usage"],
            )
            for kont in KONTEYNERY:
                # .shtorka открывается кликом по карточке «Выход» — она видна
                # только на «Сети», по умолчанию активной вкладке (сцена
                # 25_shtorka_ruchnoy или обычная 4_rabotaet — обеим годится).
                imya, bedy = proverit_odin_konteyner(br, port, kont, papka_snimkov)
                vse_bedy.extend(bedy)
            br.close()
        srv.shutdown()
    return vse_bedy


def sdelat_porchennuyu_kopiyu(rabochaya_papka):
    """Копия облика с вырезанным общим блоком правил скроллбара — та самая
    беда, на которую жаловался хозяин, воспроизведённая нарочно."""
    if rabochaya_papka.exists():
        shutil.rmtree(rabochaya_papka)
    shutil.copytree(osn.KOREN / "internal" / "sluzhba" / "oblik", rabochaya_papka)
    put = rabochaya_papka / "index.html"
    tekst = put.read_text(encoding="utf-8")
    porchennyy, zamen = BLOK_RE.subn("/* блок правил скроллбара снят щупом-контролем */", tekst)
    if zamen != 1:
        raise SystemExit(
            f"кontrol не смог вырезать блок правил (сентинелов найдено {zamen}, "
            "ожидалась 1 пара) — правь регэксп или сентинелы разошлись с файлом")
    put.write_text(porchennyy, encoding="utf-8")
    return rabochaya_papka


def main():
    kontrol = "--kontrol" in sys.argv
    xvfb = zapustit_xvfb()
    try:
        papka = KOREN / ".stend" / "oblik_polosa"
        if kontrol:
            porcha = sdelat_porchennuyu_kopiyu(KOREN / ".stend" / "oblik_polosa_porcha")
            print(f"КОНТРОЛЬ: облик с вырезанным блоком правил скроллбара — {porcha}")
            bedy = progon(porcha, papka / "kontrol")
            if bedy:
                print(f"\n🟢 КОНТРОЛЬ ПРОШЁЛ: щуп покраснел на порченом облике ({len(bedy)} бед):")
                for b in bedy:
                    print(f"  🔴 {b}")
                return 1
            print("\n🔴 КОНТРОЛЬ ПРОВАЛЕН: щуп остался зелёным на заведомо порченом "
                  "облике — он ничего не ловит, чинить нечего доказывать.")
            return 2
        oblik_dir = Path(os.environ.get("KELEVRA_OBLIK") or (KOREN / "internal" / "sluzhba" / "oblik"))
        print(f"облик: {oblik_dir}")
        bedy = progon(oblik_dir, papka)
        if bedy:
            print(f"\nКРАСНО: {len(bedy)} бед с полосой прокрутки.")
            for b in bedy:
                print(f"  🔴 {b}")
            return 1
        print("\n🟢 ЗЕЛЕНО: во всех четырёх контейнерах полоса прокрутки — наша, "
              "тёмная, без стрелок.")
        return 0
    finally:
        if xvfb is not None:
            xvfb.terminate()


if __name__ == "__main__":
    sys.exit(main())
