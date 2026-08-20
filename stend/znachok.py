#!/usr/bin/env python3
"""Рисует значок приложения: cmd/kelevra/znachok.ico.

Зачем скрипт, а не просто файл. Значок — часть облика, а облик меняется:
двоичный файл, который никто не умеет пересобрать, через месяц становится
неприкасаемым. Здесь он выводится из тех же цветов, что и окно
(тёмный фон #0d1117, зелёный акцент #3fb950), поэтому меняется вместе с ним.

Форма повторяет щит с галочкой из шапки окна: в трее у человека будут висеть
десятки значков, и узнаваем тот, который он уже видел в приложении.

Запуск: python3 stend/znachok.py   (нужен PIL)
"""
import os

from PIL import Image, ImageDraw

KOREN = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
VYHOD = os.path.join(KOREN, "cmd", "kelevra", "znachok.ico")

ZELENYY = (63, 185, 80, 255)   # #3fb950 — акцент окна
TEMNYY = (13, 17, 23, 255)     # #0d1117 — фон окна

# Рисуем крупно и уменьшаем: у щита косые кромки, без сглаживания они рвутся.
R = 1024


def shchit(d, otstup, cvet):
    """Щит: прямые плечи сверху, сходящиеся кромки снизу, скруглённый низ."""
    w = R - 2 * otstup
    x0, y0 = otstup, otstup
    x1, y1 = R - otstup, R - otstup
    plecho = y0 + w * 0.30          # докуда борта идут отвесно
    talia = y0 + w * 0.62           # где кромки начинают сходиться
    d.polygon(
        [
            (x0, plecho), (x0, y0 + w * 0.10),
            (R / 2, y0), (x1, y0 + w * 0.10),
            (x1, plecho), (x1, talia),
            (R / 2, y1), (x0, talia),
        ],
        fill=cvet,
    )
    # Верхние углы у щита срезаны, иначе он читается как «домик».
    d.rounded_rectangle([x0, y0, x1, y0 + w * 0.30], radius=w * 0.10, fill=cvet)


def narisovat():
    holst = Image.new("RGBA", (R, R), (0, 0, 0, 0))
    d = ImageDraw.Draw(holst)
    shchit(d, int(R * 0.08), ZELENYY)
    shchit(d, int(R * 0.155), TEMNYY)
    # Галочка. Толщина в долях холста: на 16x16 она обязана остаться видимой.
    tolshchina = int(R * 0.085)
    d.line(
        [(R * 0.34, R * 0.49), (R * 0.45, R * 0.60), (R * 0.67, R * 0.38)],
        fill=ZELENYY, width=tolshchina, joint="curve",
    )
    # Скругляем концы галочки: ImageDraw.line их оставляет рублеными.
    for x, y in ((R * 0.34, R * 0.49), (R * 0.67, R * 0.38)):
        d.ellipse([x - tolshchina / 2, y - tolshchina / 2,
                   x + tolshchina / 2, y + tolshchina / 2], fill=ZELENYY)
    return holst


def dib(kartinka):
    """Одна картинка значка в СТАРОМ формате (BITMAPINFOHEADER + BGRA + маска).

    Почему не PNG внутри .ico, как пишет PIL по умолчанию: значок в трее
    создаётся из этих же байтов вызовом CreateIconFromResourceEx, и PNG внутри
    значка понимают не все реализации — в том числе wine, на котором стоит мой
    единственный стенд. Старый DIB понимают все, и стенд тогда меряет продукт,
    а не свою собственную неполноту.
    """
    w, h = kartinka.size
    tochki = list(kartinka.convert("RGBA").getdata())
    zagolovok = __import__("struct").pack(
        "<IiiHHIIiiII", 40, w, h * 2, 1, 32, 0, w * h * 4, 0, 0, 0, 0)
    cvet = bytearray()
    for y in range(h - 1, -1, -1):          # DIB хранится снизу вверх
        for x in range(w):
            r, g, b, a = tochki[y * w + x]
            cvet += bytes((b, g, r, a))
    # Маска прозрачности: для 32-битного значка не используется, но её длину
    # Windows всё равно ждёт — строки выровнены по 4 байта.
    stroka = ((w + 31) // 32) * 4
    maska = bytes(stroka * h)
    return bytes(zagolovok) + bytes(cvet) + maska


def sobrat_ico(kartinki):
    struct = __import__("struct")
    kuski = [dib(k) for k in kartinki]
    shapka = struct.pack("<HHH", 0, 1, len(kuski))
    smeshchenie = 6 + 16 * len(kuski)
    katalog = b""
    for k, kusok in zip(kartinki, kuski):
        w, h = k.size
        katalog += struct.pack("<BBBBHHII", w % 256, h % 256, 0, 0, 1, 32,
                               len(kusok), smeshchenie)
        smeshchenie += len(kusok)
    return shapka + katalog + b"".join(kuski)


def main():
    holst = narisovat()
    razmery = [(48, 48), (32, 32), (16, 16)]  # 256 не кладём: тут он весит 270 КБ, а трею нужны 16 и 32
    kartinki = [holst.resize(r, Image.LANCZOS) for r in razmery]
    with open(VYHOD, "wb") as f:
        f.write(sobrat_ico(kartinki))
    print("значок:", VYHOD, os.path.getsize(VYHOD), "байт,",
          "размеры:", ", ".join(f"{w}x{h}" for w, h in razmery))
    # Показать, что мелкий размер не превратился в кашу: доля непрозрачных точек.
    for w, _ in ((16, 16), (32, 32)):
        m = holst.resize((w, w), Image.LANCZOS)
        vidno = sum(1 for p in m.getdata() if p[3] > 32)
        print(f"  {w}x{w}: непрозрачных точек {vidno} из {w * w}")


if __name__ == "__main__":
    main()
