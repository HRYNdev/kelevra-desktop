#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Стенд «значок один на все размеры»: кольцо обязано занимать ОДНУ И ТУ ЖЕ
долю ширины в каждом кадре .ico.

Зачем. 01.09 хозяин, поставив 0.6.44, написал:
  «почему ты искревил наш значок на панеле задач и в трее и сверху у
   приложения да и ваще он чет на экране как минимум дублируется еще и
   теперь разные, не понял зачем ты жирным круг сделал у значка».
Он смотрел на один и тот же .ico — но кадры внутри него были нарисованы
РАЗНО. 30.08 в генератор попал «хинтинг»: strokeWidth для каждого размера
считался как max(RING_W, HINT_MIN_STROKE_PX*FRAME/sz), 31.08 порог подняли
до 3 px и правка наконец доехала в файл. Итог в долях ширины значка:
16px — 18.8 %, 24px — 12.5 %, 32px — 9.4 %, 48px и выше — канонические 5.6 %.
Панель задач и трей берут мелкие кадры, Проводник — крупные, и человек видел
на экране два разных значка сразу. Хинтинг отменён; этот стенд стережёт,
чтобы он (или любой другой «подгоним только мелкий размер») не вернулся.

Что меряет (свойство картинки, а не «файл поменялся»):
  1. ДОЛЮ ОЛИВЫ в каждом кадре .ico. У одного рисунка, уменьшенного в разные
     размеры, эта доля почти постоянна (площадь кольца / площадь кадра);
     утолщение кольца на отдельном размере ломает её сразу и заметно.
  2. Отклонение каждого кадра от эталонного 256-го — не больше POROG_DOLI.
  3. Кольцо на месте вообще: олива есть в каждом кадре (иначе «все кадры
     одинаково пусты» тоже прошло бы проверку 1-2).
  4. Набор размеров: те, в которых Windows реально показывает значок.

Контроль (без него зелёный ничего не значит): стенд сам собирает во временный
файл .ico, где кадры 16 и 24 нарисованы С УТОЛЩЁННЫМ кольцом — ровно тот
отпечаток, что был у хинтинга, — и требует, чтобы проверки на нём ПОКРАСНЕЛИ.
Контроль зелёный — прибор слеп, весь прогон красный.

Запуск: python3 stend/znachok_odin.py [путь/к/znachok.ico]
Код возврата: 0 — зелёный, 1 — красный.
"""
import colorsys
import os
import sys
import tempfile

from PIL import Image, ImageDraw

KOREN = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.join(KOREN, "cmd", "kelevra"))
import znachok_iz_kanona as Z  # noqa: E402

# Порог отклонения доли оливы от эталонного 256-го кадра, в долях площади.
# Замер 01.09: у одного рисунка разброс по всем размерам 0.9 п.п.
# (мелкие кадры чуть «жирнее» из-за антиалиасинга — часть полупрозрачных
# точек проходит порог насыщенности). У хинтинга при пороге 3 px кадр 16px
# уходил от 256-го на 37 п.п. Порог 4 п.п. стоит посередине, ближе к замеру
# живого разброса, а не к потолку.
POROG_DOLI = 0.04
# Минимальная доля оливы: кольцо есть вообще. Площадь кольца в кадре 72 при
# RING_W=4 и RING_R=27 — 13.1 %; ниже 8 % значит, что кольцо потерялось.
POROG_ESTX = 0.08
# Размеры, которые обязаны быть в файле: те, что Windows реально показывает
# (заголовок окна и Alt-Tab — 16; трей и панель задач — 20/24/32/40;
# Проводник — 48 и 256). Список не «пусть будет побольше»: каждого размера,
# которого в .ico нет, Windows делает СВОЙ кадр своим быстрым фильтром — то
# есть рисует значок за нас, и опять по-другому.
OBYAZATELNYE = [16, 20, 24, 32, 40, 48, 256]


def semya(r, g, b, a):
    """'oliva' | 'temnoe' | None(прозрачное) — то же семейство, что в znachok_cvet.py."""
    if a < 128:
        return None
    h, s, v = colorsys.rgb_to_hsv(r / 255, g / 255, b / 255)
    if s >= 0.25 and v >= 0.25 and 60 <= h * 360 <= 110:
        return "oliva"
    return "temnoe"


def razmery_v_ico(put):
    """Какие размеры реально лежат в .ico (по каталогу образов, а не по вере)."""
    with Image.open(put) as im:
        return sorted({w for w, h in im.info["sizes"] if w == h})


def kadr(put, sz):
    im = Image.open(put)
    im.size = (sz, sz)
    return im.convert("RGBA")


def dolya_olivy(im):
    """Доля точек оливы от площади кадра."""
    oliva = sum(1 for px in im.getdata() if semya(*px) == "oliva")
    return oliva / (im.width * im.height)


def proverit(put, vsluh=True):
    """Возвращает список бед ([] — значит зелёный)."""
    bedy = []
    razmery = razmery_v_ico(put)
    net = [sz for sz in OBYAZATELNYE if sz not in razmery]
    if net:
        bedy.append(f"в .ico нет кадров {net} — эти размеры Windows нарисует сама, по-своему")

    doli = {sz: dolya_olivy(kadr(put, sz)) for sz in razmery}
    if 256 not in doli:
        bedy.append("нет кадра 256 — не с чем сверять остальные")
        return bedy
    etalon = doli[256]

    if vsluh:
        print("  размеры в файле:", razmery)
        for sz in razmery:
            print(f"  {sz:>3}px: олива {doli[sz] * 100:5.1f}% площади кадра "
                  f"(отклонение от 256-го {abs(doli[sz] - etalon) * 100:+5.1f} п.п.)")

    for sz in razmery:
        if doli[sz] < POROG_ESTX:
            bedy.append(f"в кадре {sz}px оливы всего {doli[sz] * 100:.1f}% — кольца в нём нет")
        otklon = abs(doli[sz] - etalon)
        if otklon > POROG_DOLI:
            bedy.append(
                f"кадр {sz}px нарисован не так, как 256-й: оливы {doli[sz] * 100:.1f}% "
                f"против {etalon * 100:.1f}%, разница {otklon * 100:.1f} п.п. "
                f"(порог {POROG_DOLI * 100:.0f}) — значок на этом размере другой")
    return bedy


def sobrat_s_hintingom(put):
    """Собирает .ico с ОТПЕЧАТКОМ хинтинга: кадры 16 и 24 с утолщённым кольцом.

    Не вызов удалённого кода, а его подделка ровно того же вида: поверх общего
    кадра дорисовывается кольцо шириной, которую хинтинг давал на этом размере
    (HINT_MIN_STROKE_PX=3 -> ring_w = 3*FRAME/sz в единицах кадра). Прибор
    обязан покраснеть на этом файле, иначе его зелёный ничего не значит.
    """
    kanon = Z.kadr_kanona()
    masshtab = kanon.width / Z.FRAME  # кадр нарисован в единицах FRAME, увеличенных в SS раз
    imgs = []
    for sz in Z.ICO_SIZES:
        if sz <= 24:
            zhirnyy = kanon.copy()
            ring_w = 3.0 * Z.FRAME / sz  # ровно формула хинтинга
            c = kanon.width / 2
            rad = Z.RING_R * masshtab
            w = max(1, int(round(ring_w * masshtab)))
            ImageDraw.Draw(zhirnyy).ellipse(
                [c - rad, c - rad, c + rad, c + rad],
                outline=Z.RING_START + (255,), width=w)
            imgs.append(zhirnyy.resize((sz, sz), Image.LANCZOS))
        else:
            imgs.append(Z.render_size(sz, kanon))
    imgs[-1].save(put, format="ICO",
                  sizes=[(i.width, i.height) for i in imgs], append_images=imgs[:-1])


def main():
    put = sys.argv[1] if len(sys.argv) > 1 else os.path.join(KOREN, "cmd", "kelevra", "znachok.ico")
    print(f"── значок: {put}")
    if not os.path.exists(put):
        print("  КРАСНЫЙ: файла нет")
        return 1
    bedy = proverit(put)

    print("── контроль: собираю .ico с утолщённым кольцом на 16 и 24 px, жду КРАСНОГО")
    vremenno = os.path.join(tempfile.mkdtemp(), "s_hintingom.ico")
    sobrat_s_hintingom(vremenno)
    bedy_kontrolya = proverit(vremenno)
    if bedy_kontrolya:
        print(f"  контроль краснеет как надо: {bedy_kontrolya[0]}")
    else:
        print("  КРАСНЫЙ: контроль ЗЕЛЁНЫЙ — прибор слеп, его зелёный ничего не значит")
        bedy.append("контроль порчей не покраснел")

    if bedy:
        print("🔴 КРАСНЫЙ:")
        for b in bedy:
            print("  -", b)
        return 1
    print("🟢 зелёный: все кадры .ico — один и тот же рисунок, кольцо везде одной толщины")
    return 0


if __name__ == "__main__":
    sys.exit(main())
