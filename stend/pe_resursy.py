#!/usr/bin/env python3
"""Парсер PE-ресурсов без сторонних библиотек: только на голом struct.

Зачем свой парсер, а не objdump/rsrc-читалка. Стенду нужно доказать, что в
СОБРАННОМ .exe реально лежат ресурсы RT_GROUP_ICON (14) и RT_ICON (3), а не
поверить на слово тому же инструменту, которым эти ресурсы туда клали
(cmd/kelevra/znachok_windows.syso сделан github.com/akavel/rsrc) — щуп и
вещь не должны быть одним и тем же кодом, иначе ошибка инструмента подпишет
сама себя как «всё хорошо».

Формат читается по открытой спецификации Microsoft PE/COFF:
  DOS-заголовок → e_lfanew (offset 0x3C) → сигнатура "PE\\0\\0" →
  COFF file header (20 байт) → Optional header (Data Directory[2] = таблица
  ресурсов, RVA+размер) → таблица секций (ищем секцию, куда попадает эта RVA,
  обычно .rsrc) → дерево IMAGE_RESOURCE_DIRECTORY: уровень 1 — тип ресурса,
  уровень 2 — имя/id, уровень 3 — язык → лист IMAGE_RESOURCE_DATA_ENTRY.
Внутри дерева ресурсов все смещения (кроме RVA в самом листе, который здесь
не используется) отсчитываются от начала секции ресурсов, а не от начала
файла — это прямо оговорено в спецификации ("all the offsets... are relative
to the resource directory").

Запуск: python3 stend/pe_resursy.py путь/к/Kelevra.exe
Печатает найденные типы ресурсов и код возврата: 0 — есть и 14, и 3;
1 — нет одного из них (или файл не PE вовсе).
"""
import struct
import sys

RT_ICON = 3
RT_GROUP_ICON = 14
IMENA_TIPOV = {1: "RT_CURSOR", 2: "RT_BITMAP", 3: "RT_ICON", 4: "RT_MENU",
               5: "RT_DIALOG", 6: "RT_STRING", 14: "RT_GROUP_ICON",
               16: "RT_VERSION", 24: "RT_MANIFEST"}


def naiti_sektsiyu(sektsii, rva):
    """file-offset секции, в чей виртуальный диапазон попадает rva, иначе None."""
    for imya, virt_adr, virt_razmer, file_off in sektsii:
        if virt_adr <= rva < virt_adr + max(virt_razmer, 1):
            return file_off + (rva - virt_adr)
    return None


def razobrat_pe(data):
    """Возвращает {type_id: количество листьев (образов)} по дереву ресурсов."""
    if len(data) < 0x40 or data[0:2] != b"MZ":
        raise ValueError("не PE/MZ файл (нет сигнатуры MZ)")
    e_lfanew = struct.unpack_from("<I", data, 0x3C)[0]
    if data[e_lfanew:e_lfanew + 4] != b"PE\x00\x00":
        raise ValueError("нет сигнатуры PE\\0\\0 по e_lfanew")

    coff_off = e_lfanew + 4
    (machine, kol_sektsiy, _timestamp, _symtab, _numsym,
     razmer_opt, _harakteristiki) = struct.unpack_from("<HHIIIHH", data, coff_off)
    opt_off = coff_off + 20
    if razmer_opt < 2:
        raise ValueError("optional header пуст — ресурсов не найти")

    magic = struct.unpack_from("<H", data, opt_off)[0]
    if magic == 0x10B:      # PE32
        dd_off = opt_off + 96
    elif magic == 0x20B:    # PE32+ (обычная windows/amd64 сборка Go)
        dd_off = opt_off + 112
    else:
        raise ValueError(f"неизвестный magic optional header: {magic:#x}")

    # Data Directory: 16 записей по 8 байт (RVA uint32 + Size uint32).
    # Индекс 2 — IMAGE_DIRECTORY_ENTRY_RESOURCE.
    res_dir_off = dd_off + 2 * 8
    res_rva, res_size = struct.unpack_from("<II", data, res_dir_off)
    if res_rva == 0 or res_size == 0:
        return {}

    sektsii_off = opt_off + razmer_opt
    sektsii = []
    for i in range(kol_sektsiy):
        off = sektsii_off + i * 40
        imya = data[off:off + 8].rstrip(b"\x00").decode("ascii", "replace")
        virt_razmer, virt_adr = struct.unpack_from("<II", data, off + 8)
        _razmer_raw, file_off = struct.unpack_from("<II", data, off + 16)
        sektsii.append((imya, virt_adr, virt_razmer, file_off))

    baza = naiti_sektsiyu(sektsii, res_rva)
    if baza is None:
        raise ValueError(f"RVA таблицы ресурсов {res_rva:#x} не попал ни в одну секцию")

    naideno = {}

    def kolichestvo_listyev(smeshch, est_pod):
        """Считает IMAGE_RESOURCE_DATA_ENTRY (образы) под узлом дерева рекурсивно."""
        if not est_pod:
            return 1  # сам узел — уже лист (данные, не подкаталог)
        (_har, _time, _major, _minor,
         n_named, n_id) = struct.unpack_from("<IIHHHH", data, baza + smeshch)
        vsego = n_named + n_id
        zapis_off = baza + smeshch + 16
        itogo = 0
        for i in range(vsego):
            _name_raw, data_raw = struct.unpack_from("<II", data, zapis_off + i * 8)
            pod_est = bool(data_raw & 0x80000000)
            pod_smeshch = data_raw & 0x7FFFFFFF
            itogo += kolichestvo_listyev(pod_smeshch, pod_est)
        return itogo

    # Уровень 1 (корень) — каждая запись это ТИП ресурса (RT_ICON и т.п.);
    # под ним ещё два уровня (имя/id, затем язык), считаем листья насквозь.
    (_har, _time, _major, _minor,
     n_named, n_id) = struct.unpack_from("<IIHHHH", data, baza)
    for i in range(n_named + n_id):
        name_raw, data_raw = struct.unpack_from("<II", data, baza + 16 + i * 8)
        tip_id = name_raw & 0x7FFFFFFF
        est_pod = bool(data_raw & 0x80000000)
        det_smeshch = data_raw & 0x7FFFFFFF
        naideno[tip_id] = naideno.get(tip_id, 0) + kolichestvo_listyev(det_smeshch, est_pod)
    return naideno


def main():
    if len(sys.argv) != 2:
        print("использование: pe_resursy.py путь/к/файлу.exe", file=sys.stderr)
        return 2
    with open(sys.argv[1], "rb") as f:
        data = f.read()
    try:
        naideno = razobrat_pe(data)
    except ValueError as e:
        print(f"КРАСНЫЙ: файл не разобрать как PE-ресурсы: {e}")
        return 1

    if not naideno:
        print("КРАСНЫЙ: в exe вообще нет таблицы ресурсов (.rsrc пуст или отсутствует)")
        return 1

    for tip_id in sorted(naideno):
        imya = IMENA_TIPOV.get(tip_id, f"тип {tip_id}")
        print(f"  найден ресурс {imya} (id={tip_id}): образов {naideno[tip_id]}")

    est_group = naideno.get(RT_GROUP_ICON, 0) > 0
    est_icon = naideno.get(RT_ICON, 0) > 0
    if est_group and est_icon:
        print(f"ЗЕЛЁНЫЙ: RT_GROUP_ICON есть, RT_ICON образов {naideno[RT_ICON]}")
        return 0
    nedostaet = []
    if not est_group:
        nedostaet.append("RT_GROUP_ICON (14)")
    if not est_icon:
        nedostaet.append("RT_ICON (3)")
    print(f"КРАСНЫЙ: в PE-ресурсах exe нет {' и '.join(nedostaet)} — "
          "иконка не встроена на уровне ресурсов, Проводник/панель задач/Alt-Tab "
          "покажут дефолтный значок Windows")
    return 1


if __name__ == "__main__":
    sys.exit(main())
