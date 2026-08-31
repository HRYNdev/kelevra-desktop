#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Стенд «хинтинг доехал в .ico»: мелкие кадры значка обязаны быть НАРИСОВАНЫ
отдельно, а не ужаты из 256-го.

Зачем. 30.08 хозяин сказал про значок «иконка стала слишком серая». В тот же день
в `cmd/kelevra/znachok_iz_kanona.py` появился хинтинг: каждый размер .ico
рендерится своим проходом с утолщённым кольцом, иначе линия в 0.9 px тонет в
антиалиасинге. Код был написан правильно — и не изменил .ico НИ НА ОДИН БАЙТ.
Причина: `Image.save(format="ICO", sizes=[...])` берёт ОДИН переданный образ и
ужимает его сам под каждый размер; все отдельные проходы выбрасываются молча.
Нужен `append_images`. Сутки правка считалась выкаченной, а человек видел
старую серую иконку. Отпечаток этой беды и ловит стенд.

Чем меряет (не «файл поменялся», а свойство картинки):
  1. доля ОЛИВЫ в кадре 16px — у нарисованного отдельно кольца она кратно выше,
     чем у ужатого 256-го (замер 31.08: 128 точек против 36 из 256);
  2. олива обязана перевешивать тёмное ядро на 16 и 24 px — ровно это и значит
     «не серая»;
  3. кадр 16px не должен совпадать с простым LANCZOS-уменьшением 256-го.
Пороги стоят посередине между двумя ЗАМЕРЕННЫМИ состояниями, а не взяты с
потолка.

Контроль (без него зелёный ничего не значит): стенд сам собирает во временный
файл .ico СТАРЫМ способом — без append_images — и требует, чтобы проверки на
нём ПОКРАСНЕЛИ. Если контроль зелёный, прибор слеп и весь прогон красный.

Запуск: python3 stend/znachok_hint.py [путь/к/znachok.ico]
Код возврата: 0 — зелёный, 1 — красный.
"""
import colorsys
import os
import sys
import tempfile

from PIL import Image

KOREN = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.join(KOREN, "cmd", "kelevra"))
import znachok_iz_kanona as Z  # noqa: E402

POROG_OLIVY_16 = 80      # точек оливы в кадре 16x16 (было 36 ужатием, стало 128)
POROG_RAZLICHIYA = 6.0   # средняя разница канала с ужатым 256-м, из 255


def semya(r, g, b, a):
    """'oliva' | 'temnoe' | None(прозрачное) — то же семейство, что в znachok_cvet.py."""
    if a < 128:
        return None
    h, s, v = colorsys.rgb_to_hsv(r / 255, g / 255, b / 255)
    if s >= 0.25 and v >= 0.25 and 60 <= h * 360 <= 110:
        return "oliva"
    return "temnoe"


def kadr(put, sz):
    im = Image.open(put)
    im.size = (sz, sz)
    return im.convert("RGBA")


def doli(im):
    oliva = temnoe = 0
    for px in im.getdata():
        s = semya(*px)
        if s == "oliva":
            oliva += 1
        elif s == "temnoe":
            temnoe += 1
    return oliva, temnoe


def raznica(a, b):
    """Средняя разница по каналам двух картинок одного размера."""
    pa, pb = list(a.getdata()), list(b.getdata())
    summa = sum(abs(x - y) for ta, tb in zip(pa, pb) for x, y in zip(ta, tb))
    return summa / (len(pa) * 4)


def proverit(put, vsluh=True):
    """Возвращает список бед ([] — значит зелёный)."""
    bedy = []
    k16 = kadr(put, 16)
    k24 = kadr(put, 24)
    k256 = kadr(put, 256)

    ol16, tm16 = doli(k16)
    ol24, tm24 = doli(k24)
    uzhatyy = k256.resize((16, 16), Image.LANCZOS)
    r = raznica(k16, uzhatyy)

    if vsluh:
        print(f"  16px: олива {ol16}, тёмное {tm16}; 24px: олива {ol24}, тёмное {tm24}")
        print(f"  16px против ужатого 256-го: средняя разница {r:.1f}/255")

    if ol16 < POROG_OLIVY_16:
        bedy.append(f"оливы в кадре 16px всего {ol16} (порог {POROG_OLIVY_16}) — "
                    f"кольцо тонет, значок читается серым")
    if ol16 <= tm16:
        bedy.append(f"на 16px тёмное ядро не слабее оливы ({tm16} против {ol16})")
    if ol24 <= tm24:
        bedy.append(f"на 24px тёмное ядро не слабее оливы ({tm24} против {ol24})")
    if r < POROG_RAZLICHIYA:
        bedy.append(f"кадр 16px почти совпадает с уменьшенным 256-м (разница {r:.1f}) — "
                    f"хинтинг в файл не доехал, похоже на save() без append_images")
    return bedy


def sobrat_starym_sposobom(put):
    """Собирает .ico ровно так, как это делалось до 31.08 — без append_images."""
    imgs = [Z.render_size(sz) for sz in Z.ICO_SIZES]
    imgs[-1].save(put, format="ICO", sizes=[(i.width, i.height) for i in imgs])


def main():
    put = sys.argv[1] if len(sys.argv) > 1 else os.path.join(KOREN, "cmd", "kelevra", "znachok.ico")
    print(f"── значок: {put}")
    if not os.path.exists(put):
        print("  КРАСНЫЙ: файла нет")
        return 1
    bedy = proverit(put)

    print("── контроль: собираю .ico старым способом (без append_images), жду КРАСНОГО")
    vremenno = os.path.join(tempfile.mkdtemp(), "staryy_sposob.ico")
    sobrat_starym_sposobom(vremenno)
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
    print("🟢 зелёный: мелкие кадры нарисованы отдельно, олива перевешивает тёмное")
    return 0


if __name__ == "__main__":
    sys.exit(main())
