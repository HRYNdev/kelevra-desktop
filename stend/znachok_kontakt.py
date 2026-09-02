#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Контактный лист значка: кадры ИЗ .ico на тёмной и светлой панели.

Щуп ВИДА, а не тест: вердикт выносится глазами, автоматика тут ничего не
решает. Нужен потому, что жалоба 30.08 на слишком серый значок
проверяется только взглядом на мелкие размеры — 16/24/32 px, ровно те, что
Windows берёт для панели задач и трея.

🔴 Смотрит именно .ico, а НЕ вызов render_size(). Разница не косметическая:
до 31.08 генератор рисовал каждый размер отдельным проходом с хинтингом, а в
файл клал простые уменьшения 256-го (PIL без append_images), — то есть щуп на
render_size() показывал бы картинку, которой в собранном .exe нет. Судить надо
то, что реально уезжает человеку.

Слева 1:1 (как видит человек), справа 6x nearest — чтобы было видно, ЧТО
именно тонет: толщина линии, цвет, соотношение оливы и тёмного ядра.

Запуск:
  python3 stend/znachok_kontakt.py [novyy.ico] [staryy.ico] [--out файл.png]
По умолчанию новый — cmd/kelevra/znachok.ico, старый — его же версия из HEAD.
"""
import os
import subprocess
import sys
import tempfile

from PIL import Image, ImageDraw, ImageFont

KOREN = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SIZES = [16, 24, 32, 48]
ZOOM = 6
PANELS = [("тёмная панель #202020", (0x20, 0x20, 0x20)),
          ("светлая панель #F0F0F0", (0xF0, 0xF0, 0xF0))]
SHRIFT = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"


def shrift(kegl):
    try:
        return ImageFont.truetype(SHRIFT, kegl)
    except OSError:
        return ImageFont.load_default()


def kadry(put_ico):
    """Достаёт из .ico кадры нужных размеров ровно так, как их видит Windows."""
    out = {}
    for sz in SIZES:
        im = Image.open(put_ico)
        im.size = (sz, sz)  # PIL отдаёт кадр этого размера, если он в файле есть
        out[sz] = im.convert("RGBA")
    return out


def polosa(imgs, fon, podpis):
    h = 96
    w = 270
    for sz in SIZES:
        w += sz + 8 + sz * ZOOM + 24
    holst = Image.new("RGB", (w, h), fon)
    d = ImageDraw.Draw(holst)
    tekst = (0xE8, 0xE8, 0xE8) if sum(fon) < 300 else (0x18, 0x18, 0x18)
    d.text((10, h // 2 - 8), podpis, fill=tekst, font=shrift(13))
    x = 270
    for sz in SIZES:
        im = imgs[sz]
        holst.paste(im, (x, (h - sz) // 2), im)
        d.text((x, h - 18), str(sz), fill=tekst, font=shrift(11))
        x += sz + 8
        big = im.resize((sz * ZOOM, sz * ZOOM), Image.NEAREST)
        holst.paste(big, (x, (h - sz * ZOOM) // 2), big)
        x += sz * ZOOM + 24
    return holst


def main():
    argv = [a for a in sys.argv[1:] if not a.startswith("--")]
    out = "/opt/jarvis-goal/telo/dannye/znachok_kontakt_v4.png"
    if "--out" in sys.argv:
        out = sys.argv[sys.argv.index("--out") + 1]
    novyy = argv[0] if argv else os.path.join(KOREN, "cmd", "kelevra", "znachok.ico")
    if len(argv) > 1:
        staryy = argv[1]
    else:
        staryy = os.path.join(tempfile.mkdtemp(), "staryy.ico")
        with open(staryy, "wb") as f:
            subprocess.run(["git", "show", "HEAD:cmd/kelevra/znachok.ico"],
                           cwd=KOREN, stdout=f, check=True)

    pary = [("ДО  (HEAD) ", kadry(staryy)), ("ПОСЛЕ      ", kadry(novyy))]
    stroki = []
    for imya, fon in PANELS:
        for podpis, imgs in pary:
            stroki.append(polosa(imgs, fon, podpis + imya))
    w = max(p.width for p in stroki)
    h = sum(p.height for p in stroki)
    list_ = Image.new("RGB", (w, h), (0, 0, 0))
    y = 0
    for p in stroki:
        list_.paste(p, (0, y))
        y += p.height
    list_.save(out)
    print("написано:", out, list_.size)


if __name__ == "__main__":
    main()
