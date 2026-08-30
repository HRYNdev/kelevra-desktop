#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Рисует znachok.ico (иконку Windows-сборки Kelevra) по ЕГО канону — вектору
Android-иконки из основного репозитория, а НЕ по устаревшему растру.

Источник чисел (читать, не редактировать):
  /opt/jarvis-goal/repos/prizhilos_repo/kelevra-box/app/src/main/res/drawable/ic_launcher_foreground.xml
  /opt/jarvis-goal/repos/prizhilos_repo/kelevra-box/app/src/main/res/values/ic_launcher_background.xml

Почему это понадобилось (28.08, голос хозяина):
  "взято неправильно иконка... взято старая иконка, мы уже переделали". 25.08 он
  переписал вектор на оливу (#A8CC6B -> #7F9E4A), а растровые mipmap-*.png в
  kelevra-box с 07.08 не перегенерированы и остались БИРЮЗОВЫМИ. Раньше
  znachok.ico был собран из этого устаревшего растра — отсюда бирюза в
  Kelevra.exe. Правка: рисуем заново по вектору, растр больше не используем.

Геометрия (viewport 108x108, как в векторе):
  - фон:                 ПРОЗРАЧНЫЙ (alpha 0) — правка 30.08, хозяин: «иконку
                          нормально круглой сделать, че это за квадрат».
                          #111310 был не фоном приложения (там нет системной
                          рамки, куда он мог бы «слиться»), а сплошным
                          квадратом, который Проводник/панель задач рисуют
                          КАК ЕСТЬ — отсюда и квадрат вокруг круглого кольца
                          на скрине. Кольцо (и его внутреннее свечение)
                          теперь единственная непрозрачная фигура — контур
                          иконки становится круглым сам по себе, без маски.
  - кольцо:               центр (54,54), радиус 27, strokeWidth 4,
                           обводка линейным градиентом (27,27)->(81,81):
                           #A8CC6B -> #7F9E4A, заливка прозрачная
  - внутреннее свечение:  круг центр (54,54), радиус 19, радиальный градиент
                           от центра #A8CC6B@alpha=0x80 до alpha=0 на радиусе 19,
                           весь слой умножен на fillAlpha=0.16

Кадр (72 из 108):
  Android adaptive-icon обрезает foreground до центральных ~2/3 (эквивалент
  центрального квадрата 72x72 из 108x108 viewport) — так кольцо и было видно
  на телефоне. У Windows .ico маски adaptive-icon нет, поэтому чтобы кадр
  (какую долю ширины занимает кольцо) не поменялся при замене бирюзы на
  оливу, здесь тоже берём центральный квадрат 72x72 из холста 108x108 и его
  масштабируем на все размеры .ico. Это НЕ произвольное решение — это тот же
  кроп, что уже был у бирюзовой иконки (проверено щупом palette_probe).

Рисуется с суперсэмплингом 8x (в 864x864) и уменьшается LANCZOS'ом, чтобы
антиалиасинг кольца был гладким на маленьких размерах .ico.
"""
import os
from PIL import Image, ImageDraw

VIEWPORT = 108
FRAME = 72  # центральный кроп 72 из 108 (2/3), как у adaptive-icon
SS = 8  # суперсэмплинг

BG = (0x11, 0x13, 0x10, 0x00)  # alpha 0: круглая иконка, не квадрат (см. докстринг)
RING_START = (0xA8, 0xCC, 0x6B)  # #A8CC6B, у (27,27) на viewport
RING_END = (0x7F, 0x9E, 0x4A)    # #7F9E4A, у (81,81) на viewport
GLOW_COLOR = (0xA8, 0xCC, 0x6B)  # #A8CC6B
GLOW_FILL_ALPHA = 0.16
GLOW_CENTER_ALPHA = 0x80

CX, CY = 54.0, 54.0
RING_R = 27.0
RING_W = 4.0
GLOW_R = 19.0

OUT_ICO = os.path.join(os.path.dirname(__file__), "znachok.ico")
ICO_SIZES = [16, 24, 32, 48, 64, 128, 256]


def lerp(a, b, t):
    return a + (b - a) * t


def ring_color_at(px, py, s):
    """Цвет линейного градиента (27,27)->(81,81) в точке (px,py) в масштабе s."""
    x0, y0 = 27 * s, 27 * s
    x1, y1 = 81 * s, 81 * s
    dx, dy = x1 - x0, y1 - y0
    denom = dx * dx + dy * dy
    t = ((px - x0) * dx + (py - y0) * dy) / denom
    t = max(0.0, min(1.0, t))
    r = lerp(RING_START[0], RING_END[0], t)
    g = lerp(RING_START[1], RING_END[1], t)
    b = lerp(RING_START[2], RING_END[2], t)
    return (r, g, b)


def draw_canvas(s):
    """Рисует холст VIEWPORT*s x VIEWPORT*s (RGBA)."""
    size = int(VIEWPORT * s)
    base = Image.new("RGBA", (size, size), BG)

    # --- внутреннее свечение: радиальный градиент, умноженный на fillAlpha 0.16
    glow = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    gpx = glow.load()
    gr = GLOW_R * s
    gcx, gcy = CX * s, CY * s
    x0 = max(0, int(gcx - gr) - 2)
    x1 = min(size, int(gcx + gr) + 2)
    y0 = max(0, int(gcy - gr) - 2)
    y1 = min(size, int(gcy + gr) + 2)
    for y in range(y0, y1):
        for x in range(x0, x1):
            d = ((x + 0.5 - gcx) ** 2 + (y + 0.5 - gcy) ** 2) ** 0.5
            if d > gr:
                continue
            t = d / gr
            a = lerp(GLOW_CENTER_ALPHA, 0, t) * GLOW_FILL_ALPHA
            gpx[x, y] = (GLOW_COLOR[0], GLOW_COLOR[1], GLOW_COLOR[2], int(round(a)))
    base = Image.alpha_composite(base, glow)

    # --- кольцо: заливка прозрачная, только обводка width=4 градиентом
    ring = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    rpx = ring.load()
    r_out = (RING_R + RING_W / 2) * s
    r_in = (RING_R - RING_W / 2) * s
    rcx, rcy = CX * s, CY * s
    x0 = max(0, int(rcx - r_out) - 2)
    x1 = min(size, int(rcx + r_out) + 2)
    y0 = max(0, int(rcy - r_out) - 2)
    y1 = min(size, int(rcy + r_out) + 2)
    for y in range(y0, y1):
        for x in range(x0, x1):
            px, py = x + 0.5, y + 0.5
            d = ((px - rcx) ** 2 + (py - rcy) ** 2) ** 0.5
            if r_in <= d <= r_out:
                col = ring_color_at(px, py, s)
                rpx[x, y] = (int(round(col[0])), int(round(col[1])), int(round(col[2])), 255)
    base = Image.alpha_composite(base, ring)
    return base


def build():
    s = SS
    big = draw_canvas(s)

    # даунсемплим весь viewport 108 -> 108 с суперсэмплингом (сглаживание)
    smooth = big.resize((VIEWPORT, VIEWPORT), Image.LANCZOS)

    # кроп центрального квадрата FRAME из VIEWPORT (тот же кадр, что был у
    # бирюзовой иконки — adaptive-icon срезает края)
    off = (VIEWPORT - FRAME) // 2
    cropped = smooth.crop((off, off, off + FRAME, off + FRAME))

    imgs = []
    for sz in ICO_SIZES:
        imgs.append(cropped.resize((sz, sz), Image.LANCZOS))

    largest = imgs[-1]
    largest.save(
        OUT_ICO,
        format="ICO",
        sizes=[(i.width, i.height) for i in imgs],
    )
    return imgs


if __name__ == "__main__":
    imgs = build()
    print("написано:", OUT_ICO, "образов:", len(imgs), "размеры:", [i.size for i in imgs])
