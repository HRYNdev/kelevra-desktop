#!/usr/bin/env python3
"""Стенд «цвет иконки в PE»: доказывает, что образ RT_ICON внутри собранного
Kelevra.exe действительно оливковый (канон хозяина), а не тихо вернулся к
старому бирюзовому.

Зачем отдельный стенд, а не расширение pe_resursy.py. stend/znachok_exe.sh
уже сторожит НАЛИЧИЕ ресурсов RT_ICON/RT_GROUP_ICON, но к их СОДЕРЖИМОМУ
слеп: он видит, что образ есть, не глядя, какого он цвета. Диагноз 28.08
(голос хозяина) был именно про это — иконка молча осталась бирюзовой после
того, как вектор перекрасили в оливу (см. cmd/kelevra/znachok_iz_kanona.py),
и ни один стенд не покраснел. Здесь красный ставит именно цвет.

Что стенд делает:
  1. Достаёт из PE самый КРУПНЫЙ образ RT_ICON (тип 3) через
     pe_resursy.sobrat_obrazy — этот код реально извлекает байты листа,
     pe_resursy.razobrat_pe их только считает и не трогает.
  2. Разбирает образ в RGBA через PIL. Внутри .ico встречаются два формата:
     PNG (современные крупные образы, см. cmd/kelevra/znachok_iz_kanona.py)
     и старый BITMAPINFOHEADER+BGRA (см. bpp=32 в ICONDIRENTRY у некоторых
     сборок rsrc) — оба нужно понимать, потому что какой из них попадёт в
     конкретный .syso, зависит от того, чем и когда он был собран.
  3. Меряет долю двух семейств цвета по HSV, не по точному RGB: иконка
     сглажена антиалиасингом, точных совпадений с канон-цветом почти нет,
     а оттенок (hue) у полупрозрачных промежуточных пикселей всё равно
     близок к своему семейству.
       ОЛИВА  (канон, #A8CC6B..#7F9E4A): hue 60..110°
       БИРЮЗА (старый, отвергнутый цвет):  hue 160..200°
     Порог по насыщенности/яркости (S,V >= 0.25) отсекает тёмный фон
     #111310 — у него hue тоже возле 100° (почти олива!), но V≈7% и
     S≈16%, то есть заведомо ниже порога.
  4. Вердикт: ЗЕЛЁНЫЙ, только если оливы >= 2.0% непрозрачных точек И
     бирюзы <= 0.2%. Асимметрия порогов нарочная: немного оливы в кадре —
     это едва различимая грань кольца на маленьком образе, а даже немного
     БИРЮЗЫ — это уже смешение старого цвета с новым, чего в чистой
     перерисовке быть не должно.

Запуск: python3 stend/znachok_cvet.py [путь/к/Kelevra.exe]
По умолчанию берёт .stend_znachok/Kelevra.exe (туда кладёт exe
stend/znachok_exe.sh). Код возврата: 0 — зелёный, 1 — красный (в т.ч. файл
не PE, нет RT_ICON или формат образа не разобрать).
"""
import colorsys
import io
import os
import struct
import sys

KOREN = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.join(KOREN, "stend"))
import pe_resursy as pr  # noqa: E402

PNG_SIGNATURA = b"\x89PNG\r\n\x1a\n"
BITMAPINFOHEADER_SIZE = 40

# Оттенок (градусы 0..360) двух семейств цвета.
OLIVA_HUE = (60, 110)
BIRUZA_HUE = (160, 200)
# Ниже этих S/V пиксель не относим ни к одному семейству — иначе тёмный фон
# #111310 (hue тоже около 100°, но S≈0.16, V≈0.07) попал бы в «оливу».
MIN_S = 0.25
MIN_V = 0.25
MIN_ALPHA = 16  # почти прозрачные точки антиалиасинга в счёт не идут

PORTIT_OLIVA = 2.0   # минимум доли оливы, %, чтобы считать иконку оливковой
PORTIT_BIRUZA = 0.2  # максимум доли бирюзы, %, выше — цвет смешан/старый


def samyy_krupnyy_rt_icon(data):
    """Самый крупный по числу байт образ RT_ICON (крупный образ несёт больше
    деталей и меньше всего искажён даунскейлом — самый честный образец)."""
    obrazy = pr.sobrat_obrazy(data, pr.RT_ICON)
    if not obrazy:
        raise ValueError("в PE-ресурсах нет ни одного образа RT_ICON (id=3)")
    obrazy.sort(key=len, reverse=True)
    return obrazy[0]


def v_rgba(obraz):
    """Образ RT_ICON (сырые байты) → PIL.Image в режиме RGBA."""
    from PIL import Image

    if obraz[:8] == PNG_SIGNATURA:
        return Image.open(io.BytesIO(obraz)).convert("RGBA")

    if len(obraz) < BITMAPINFOHEADER_SIZE:
        raise ValueError(f"образ короче BITMAPINFOHEADER ({len(obraz)} байт)")
    (razmer_zag, w, h, ploskosti, bpp, szhatie,
     razmer_kartinki, *_ost) = struct.unpack_from("<IiiHHIIiiII", obraz, 0)
    if razmer_zag != BITMAPINFOHEADER_SIZE:
        raise ValueError(
            f"неизвестный заголовок образа: не PNG и не BITMAPINFOHEADER "
            f"(первые байты {obraz[:4].hex()})")
    if szhatie != 0:
        raise ValueError(f"BITMAPINFOHEADER со сжатием {szhatie} не поддержан")
    if bpp != 32:
        raise ValueError(f"BITMAPINFOHEADER с bpp={bpp} не поддержан (нужен 32)")
    # У .ico высота в заголовке — XOR-маска + AND-маска вместе, реальная
    # высота картинки вдвое меньше; данные лежат снизу вверх (стандартный DIB).
    vysota = h // 2
    if w <= 0 or vysota <= 0:
        raise ValueError(f"нулевые размеры образа (w={w}, h={h})")
    dlina_cveta = w * vysota * 4
    if razmer_zag + dlina_cveta > len(obraz):
        raise ValueError(
            f"образ короче заявленных размеров {w}x{vysota} "
            f"(нужно {razmer_zag + dlina_cveta} байт, есть {len(obraz)})")
    cvet = obraz[razmer_zag:razmer_zag + dlina_cveta]
    return Image.frombytes("RGBA", (w, vysota), cvet, "raw", "BGRA", 0, -1)


def doli_semeystv(kartinka):
    """(доля_oliva_%, доля_biruza_%, всего_neprozrachnyh).

    Через load()/getpixel, а не getdata() — на Pillow 12 getdata() уже
    помечен deprecated (уйдёт в 14), а load() стабилен на любой версии.
    """
    w, h = kartinka.size
    pixely = kartinka.load()
    vsego = oliva = biruza = 0
    for y in range(h):
        for x in range(w):
            r, g, b, a = pixely[x, y]
            if a < MIN_ALPHA:
                continue
            vsego += 1
            hue, s, v = colorsys.rgb_to_hsv(r / 255, g / 255, b / 255)
            hue *= 360
            if s < MIN_S or v < MIN_V:
                continue
            if OLIVA_HUE[0] <= hue <= OLIVA_HUE[1]:
                oliva += 1
            elif BIRUZA_HUE[0] <= hue <= BIRUZA_HUE[1]:
                biruza += 1
    if vsego == 0:
        return 0.0, 0.0, 0
    return 100 * oliva / vsego, 100 * biruza / vsego, vsego


def main():
    put = sys.argv[1] if len(sys.argv) > 1 else os.path.join(KOREN, ".stend_znachok", "Kelevra.exe")
    if not os.path.isfile(put):
        print(f"КРАСНЫЙ: файла нет: {put}")
        return 1
    with open(put, "rb") as f:
        data = f.read()

    try:
        obraz = samyy_krupnyy_rt_icon(data)
        kartinka = v_rgba(obraz)
    except ValueError as e:
        print(f"КРАСНЫЙ: не удалось разобрать цвет иконки в {put}: {e}")
        return 1

    oliva_pct, biruza_pct, vsego = doli_semeystv(kartinka)
    w, h = kartinka.size
    if vsego == 0:
        print(f"КРАСНЫЙ: образ {w}x{h} ({put}) целиком прозрачный — цвет мерить нечем")
        return 1

    zeleno = oliva_pct >= PORTIT_OLIVA and biruza_pct <= PORTIT_BIRUZA
    tsvet = "ЗЕЛЁНЫЙ" if zeleno else "КРАСНЫЙ"
    print(f"{tsvet}: образ RT_ICON {w}x{h} ({len(obraz)} байт) из {put}: "
          f"олива {oliva_pct:.2f}% (порог >= {PORTIT_OLIVA:.2f}%), "
          f"бирюза {biruza_pct:.2f}% (порог <= {PORTIT_BIRUZA:.2f}%), "
          f"непрозрачных точек {vsego} из {w * h}")
    return 0 if zeleno else 1


if __name__ == "__main__":
    sys.exit(main())
