#!/usr/bin/env python3
"""Забор ГЕОМЕТРИИ главного экрана — пять бед вёрстки, найденных на снимке
`4_rabotaet.png` и на бедах `11_beda_port.png`/`12_beda_seti.png` (24.08),
не ловятся ни одним щупом oblik_snimok.py: те смотрят на
достижимость/переполнение/обрезку/жаргон, а не на то, КАК куски экрана
читаются друг относительно друга. Мерим через getBoundingClientRect() на
реально отрендеренной сцене — тем же playwright и тем же сервером-заглушкой,
что и oblik_snimok.py (импортируем его, а не дублируем HTTP-обвязку).

Пороги (заданы человеком, не подкручены под результат):
  (а) низ круга → верх подсказки-подсказки под ним          ≤ 24px
  (б) круг→подсказка СТРОГО МЕНЬШЕ подсказка→строка ключа   (подпись обязана
      принадлежать кругу визуально, а не строке ключа доступа под ней)
  (в) горизонтальный зазор между соседними кусками строки
      «Ключ доступа: ИМЯ … действует до ДАТЫ»                ≤ 16px
  (г) низ скроллящейся ленты не заходит НИЖЕ верха панели вкладок:
      lenta.bottom <= vkladki.top (допуск 1px на округление). Раньше эта
      проверка сравнивала «верх панели» с «низом узла, обрезанным по низу
      ленты» — а низ ленты уже клипается по var(--taby-vysota) в CSS, то есть
      lenta.bottom === vkladki.top ВСЕГДА, и строгое el.top < vkladki.top <
      el.bottom не выполнялось НИ РАЗУ ни при какой разметке. Проверка
      светилась зелёным по конструкции, не по факту (диагноз 24.08).
  (д) ВЫБРАННЫЙ узел списка (класс .vybran) обязан помещаться в контейнере
      целиком: с 29.08 список живёт в шторке выбора выхода (.shtorka, см.
      (з) ниже), поэтому граница теперь её, не ленты: shtorka.top <=
      узел.top и узел.bottom <= shtorka.bottom (допуск 1px). Обрезанная
      НЕвыбранная строка — нормальный признак прокрутки, беды тут нет; но
      если обрезан именно текущий выбор — человек не видит, что у него
      выбрано (снимки 11_beda_port.png/12_beda_seti.png, 24.08: «Нидерланды
      2» обрезана ровно посередине текста; беда была найдена на главном
      экране, до переезда списка в шторку, — проверка того же класса брака
      живёт теперь в proverit_kartu_i_shtorku).
  (е) пять состояний точки сигнала узла (.uzel .signal — bystro/sredne/
      medlenno/mertv/«без замера») обязаны иметь ПОПАРНО РАЗНЫЙ вычисленный
      вид (background-color + box-shadow + border). Судим getComputedStyle,
      а не текст CSS-правила: цвета приходят из var(), и слияние видно только
      ПОСЛЕ подстановки (25.08: sredne и medlenno делили один var(--zhdem) —
      все 23 сцены стенда несли задержки 78/64/91мс, «sredne»/«medlenno» не
      попадали на экран НИ РАЗУ, и щуп это не заметил — вход был пуст).
      Смотрим только на сцене 24_uzly_vse_gradacii — единственной, где все
      пять состояний нарисованы разом.
  (ж) ОСЬ X И ПЕРЕКРЫТИЕ. (ж1) центр круга обязан совпадать с центром ОКНА
      (допуск 2px), а не с центром своего блока: <button> в Chromium держит
      shrink-to-fit ширину даже при display:flex, и align-items:center молча
      центровал круг внутри 210px, прижатых к левому краю (замер 27.08 —
      87px влево). (ж2) блок круга не смеет накрывать собой ни одну надпись
      ленты после прокрутки до конца — так ловится любое «приклеим круг»
      (sticky/fixed): непрозрачный прямоугольник едет над списком и прячет
      текст. Обе беды названы человеком, а не выведены мной: Вова 27.08 10:29
      «он нахуй всё ещё стоит нахуй не по центру а слева», «закрывает обзор
      на надписи», «ездит нахуй квадратом». Прежняя (ж) мерила уезд за
      ВЕРХНИЙ край при прокрутке — мой неверный разбор его слов 25.08.
  (з) КАРТОЧКА «ВЫХОД» + ШТОРКА ВЫБОРА (29.08, ответ на заказ Вовы 27.08:
      «должно быть тупо выбор авто режим который сам все определяет, или
      ручной и там сам выбираешь» — список узлов с «Нидерланды прямой,
      запасной, комната», висевший на экране ВСЕГДА, его злил). Полоса
      режима успела пожить на главном экране, в «Настройках» и снова на
      главном (третье место по счёту) — теперь она внутри шторки, которая
      открывается кликом по карточке «Выход» (эталон телефона: HomeScreen.kt
      293-325 карточка, 374-443 шторка). 29.08 Вова про сам список узлов,
      дословно: «ручной режим если тыкаешь то там выбор сделан криво» —
      выбранный узел получил явную галочку ✓ (ExitRow, 528-529) взамен еле
      заметной рамки.
      (з0) на главном экране (вкладка «Сеть») РОВНО одна карточка
      `.karta-vyhod`, с непустым состоянием (title/subtitle) и шевроном
      «›»; шторка ДО клика по карточке закрыта.
      (з1) клик по карточке открывает шторку: внутри ровно одна полоса
      `.rezhim-perekl` с дословным текстом «Автоматически» / «Вручную» —
      не перевод смысла, а те же слова, что на телефоне (HomeScreen.kt:
      464-465), без перекрытий с другим текстом шторки; список `#uzly`
      виден — эталон телефона, HomeScreen.kt 413: список виден в ОБОИХ
      режимах, не прячется в авто целиком, как было раньше.
      (з2) в авторежиме подсветку (.vybran) не несёт НИ ОДИН узел — эталон
      HomeScreen.kt 424 (selected = !auto.auto && ch.selected); в ручном —
      подсвечен ровно один узел (текущий выбор), и у него есть галочка ✓.
      (з3) Esc закрывает шторку, и на вкладке «Настройки» (#tab-nastroyki)
      полосы `.rezhim-perekl` нет вовсе — ловит именно возврат на прежнее
      (28.08) место.

    python3 stend/oblik_geometriya.py
"""
import json, socketserver, sys, threading
from pathlib import Path

KOREN = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(Path(__file__).resolve().parent))
from oblik_snimok import Ruchki, SCENY, sostoyanie, SHIRINA, VYSOTA  # noqa: E402

GEOMETRIYA_JS = """() => {
  // 1_kod (экран кода) не рисует круг/подсказку/строку ключа/панель вкладок
  // вовсе (display:none у скрытых узлов) — getBoundingClientRect() на таком
  // элементе честно возвращает прямоугольник 0x0, а не null. Раньше щуп
  // принимал такой нулевой прямоугольник за настоящий — отсюда ложная беда
  // «(б) круг→подсказка 0px НЕ меньше подсказка→ключ 0px» на сцене, где ни
  // круга, ни подсказки, ни строки ключа физически нет. Отсекаем такие узлы
  // здесь ОДИН раз — молчат все проверки ниже, а не только (б).
  const r = (el) => {
    if (!el) return null;
    const rr = el.getBoundingClientRect();
    return (rr.width > 0 || rr.height > 0) ? rr : null;
  };
  const krug = r(document.querySelector('.krug-fon'));
  const podskazka = r(document.getElementById('podskazka'));
  const podpiska = r(document.getElementById('podpiska'));
  const imya = r(document.getElementById('podpiska-imya'));
  const srok = r(document.getElementById('podpiska-srok'));
  const vkladki = r(document.getElementById('vkladki'));
  const lenta = r(document.getElementById('lenta'));
  return {krug, podskazka, podpiska, imya, srok, vkladki, lenta};
}"""


def proverit_glavnyy_ekran(str_, imya_sceny):
    d = str_.evaluate(GEOMETRIYA_JS)
    bedy = []

    krug, podskazka, podpiska = d["krug"], d["podskazka"], d["podpiska"]
    if krug and podskazka:
        d1 = podskazka["top"] - krug["bottom"]
        if d1 > 24:
            bedy.append(f"{imya_sceny}: (а) круг→подсказка {d1:.0f}px, а порог ≤24px "
                        f"(круг.bottom={krug['bottom']:.0f}, подсказка.top={podskazka['top']:.0f})")
        if podpiska:
            d2 = podpiska["top"] - podskazka["bottom"]
            if not (d1 < d2):
                bedy.append(f"{imya_sceny}: (б) круг→подсказка {d1:.0f}px НЕ меньше "
                            f"подсказка→ключ {d2:.0f}px — подсказка визуально не принадлежит кругу")

    imya_r, srok_r = d["imya"], d["srok"]
    if imya_r and srok_r and (imya_r["width"] or srok_r["width"]):
        zazor = srok_r["left"] - imya_r["right"]
        if zazor > 16:
            bedy.append(f"{imya_sceny}: (в) зазор в строке «Ключ доступа» {zazor:.0f}px, "
                        f"а порог ≤16px (имя.right={imya_r['right']:.0f}, срок.left={srok_r['left']:.0f})")

    lenta, vkladki = d["lenta"], d["vkladki"]
    if lenta and vkladki:
        zapolzanie = lenta["bottom"] - vkladki["top"]
        if zapolzanie > 1:
            bedy.append(f"{imya_sceny}: (г) низ ленты {lenta['bottom']:.0f}px заходит под "
                        f"панель вкладок на {zapolzanie:.0f}px (панель.top={vkladki['top']:.0f})")

    # (д) — «выбранный узел обрезан лентой» — переехала в proverit_kartu_i_shtorku
    # ниже вместе со списком узлов: с 29.08 список живёт в шторке (.shtorka),
    # а не в этой самой ленте, и мерить его тут стало нечем (шторка закрыта
    # по умолчанию, #uzly вернул бы пустой прямоугольник).
    return bedy


KRUG_OS_X_JS = """() => {
  // Меряем ДВЕ вещи, обе — с натуры, а не с догадки.
  // (ж1) центр круга против центра ОКНА. align-items:center у .krug-blok
  //      центрует внутри БЛОКА, и если блок уже окна (<button> в Chromium
  //      держит shrink-to-fit ширину даже при display:flex), круг честно
  //      стоит по центру блока и криво по центру экрана.
  // (ж2) что блок круга накрывает СОБОЙ после прокрутки ленты до конца.
  //      Ловит любое лекарство вида «приклеим круг» (sticky/fixed): такой
  //      блок непрозрачен и едет над списком, пряча текст под собой.
  const lenta = document.getElementById('lenta');
  const krug = document.querySelector('.krug-fon');
  const blok = document.querySelector('.krug-blok');
  if (!lenta || !krug || !blok) return null;
  const k = krug.getBoundingClientRect();
  if (!(k.width > 0 || k.height > 0)) return null;  // сцена без круга (1_kod)
  const out = {okno: window.innerWidth, centr: (k.left + k.right) / 2,
               left: k.left, right: k.right};
  lenta.scrollTop = lenta.scrollHeight - lenta.clientHeight;
  const b = blok.getBoundingClientRect();
  const zakryto = [];
  for (const el of lenta.querySelectorAll('*')) {
    if (blok.contains(el) || el.contains(blok)) continue;
    const r = el.getBoundingClientRect();
    if (r.width < 4 || r.height < 4) continue;
    const dy = Math.min(r.bottom, b.bottom) - Math.max(r.top, b.top);
    const dx = Math.min(r.right, b.right) - Math.max(r.left, b.left);
    // Только СВОЙ текст узла: у предка textContent несёт текст всех детей
    // разом, и один накрытый пункт дал бы десяток одинаковых жалоб.
    const svoy = [...el.childNodes].filter((n) => n.nodeType === 3)
                   .map((n) => n.textContent.trim()).join(' ').trim();
    if (dy > 2 && dx > 2 && svoy) zakryto.push({tekst: svoy.slice(0, 30), dy: Math.round(dy)});
  }
  out.zakryto = zakryto.slice(0, 4);
  return out;
}"""


def proverit_krug_os_x(str_, imya_sceny):
    """(ж) Круг стоит РОВНО по центру окна и ничего собой не закрывает.

    Прежняя (ж) мерила «не уехал ли круг за верхний край при прокрутке» — это
    был мой разбор слов Вовы 25.08 «круг куда то съехал», и разобрал я их
    неверно. Его же слова 27.08 10:29, дословно: «он нахуй всё ещё стоит нахуй
    не по центру а слева»; «при скролле ещё и нахуя то ездит дак ещё и криво
    ездит так как закрывает обзор на надписи, а ещё он ездит нахуй квадратом».
    То есть беда была по оси X (замер 27.08: 87px влево), а моё лекарство от
    выдуманной вертикальной беды (position:sticky) добавило вторую — едущий
    над списком непрозрачный прямоугольник 210x220. Прибор судит теперь то,
    что назвал человек, а не то, что придумал я.
    Порог 2px — округление разметки; 87px в него не влезает никак.
    """
    d = str_.evaluate(KRUG_OS_X_JS)
    if not d:
        return []
    bedy = []
    sdvig = d["centr"] - d["okno"] / 2
    if abs(sdvig) > 2:
        storona = "ВЛЕВО" if sdvig < 0 else "ВПРАВО"
        bedy.append(f"{imya_sceny}: (ж1) круг сдвинут {storona} на {abs(sdvig):.0f}px — "
                    f"центр круга {d['centr']:.0f}px, центр окна {d['okno'] / 2:.0f}px "
                    f"(круг {d['left']:.0f}…{d['right']:.0f})")
    if d["zakryto"]:
        chto = ", ".join(f"«{z['tekst']}» на {z['dy']}px" for z in d["zakryto"])
        bedy.append(f"{imya_sceny}: (ж2) блок круга накрывает собой текст при прокрутке: {chto}")
    return bedy


REZHIM_NASTROYKI_JS = """() => {
  // (з3) полосы `.rezhim-perekl` на вкладке «Настройки» быть не должно
  // вовсе. С 29.08 полоса — потомок `.shtorka`, сестры `#tab-set`/
  // `#tab-nastroyki`, а не их содержимого, так что запрос ниже структурно
  // истинен всегда — щуп остаётся как регресс-страховка на случай, если
  // разметку снова вернут внутрь вкладки.
  return {
    barVNastroykah: document.querySelectorAll('#tab-nastroyki .rezhim-perekl').length,
  };
}"""

KARTA_GLAVNAYA_JS = """() => {
  const r = (el) => { if (!el) return null; const rr = el.getBoundingClientRect();
    return (rr.width > 0 || rr.height > 0) ? rr : null; };
  // Есть ли вообще главный экран на этой сцене — своя примета (круг),
  // независимая от карточки: иначе отсутствие `.karta-vyhod` в разметке
  // читалось бы прибором как «сцена без главного экрана» и молчало бы —
  // ровно та ловушка «проверка с пустым входом зеленеет» (25.08).
  const glavnyyEkranViden = !!r(document.querySelector('.krug-fon'));
  const karty = document.querySelectorAll('.karta-vyhod');
  const karta = karty[0] || null;
  const title = document.getElementById('vyhod-title');
  const subtitle = document.getElementById('vyhod-subtitle');
  const shtorkaFon = document.getElementById('shtorka-fon');
  return {
    glavnyyEkranViden,
    kolichestvoKart: karty.length,
    kartaVidna: !!r(karta),
    titleText: title ? title.textContent.trim() : null,
    subtitleText: subtitle ? subtitle.textContent.trim() : null,
    shevronEst: !!(karta && karta.querySelector('.vyhod-shevron')),
    shtorkaVidnaDoKlika: !!(shtorkaFon && !shtorkaFon.hidden),
  };
}"""

SHTORKA_OTKRYTA_JS = """() => {
  const r = (el) => { if (!el) return null; const rr = el.getBoundingClientRect();
    return (rr.width > 0 || rr.height > 0) ? rr : null; };
  const fon = document.getElementById('shtorka-fon');
  const shtorka = document.getElementById('shtorka');
  const bar = document.querySelector('#shtorka-fon .rezhim-perekl');
  const avto = document.getElementById('rezhim-avto');
  const ruchnoy = document.getElementById('rezhim-ruchnoy');
  const uzly = document.getElementById('uzly');
  const barRect = r(bar);
  const shtorkaRect = r(shtorka);
  // Тот же приём измерения, что и у круга (ж2)/старой полосы: перекрытие
  // полосы с ЛЮБЫМ другим текстом шторки. Потомки #uzly исключены — список
  // сам скроллится (overflow-y у .shtorka целиком, не у #uzly с 29.08).
  const perekryto = [];
  if (barRect && shtorka) {
    for (const el of shtorka.querySelectorAll('*')) {
      if (bar.contains(el) || el.contains(bar)) continue;
      if (uzly && uzly.contains(el)) continue;
      const rr = el.getBoundingClientRect();
      if (rr.width < 4 || rr.height < 4) continue;
      const dy = Math.min(rr.bottom, barRect.bottom) - Math.max(rr.top, barRect.top);
      const dx = Math.min(rr.right, barRect.right) - Math.max(rr.left, barRect.left);
      const svoy = [...el.childNodes].filter((n) => n.nodeType === 3)
                     .map((n) => n.textContent.trim()).join(' ').trim();
      if (dy > 2 && dx > 2 && svoy) perekryto.push({tekst: svoy.slice(0, 30), dy: Math.round(dy)});
    }
  }
  // top/bottom — СЫРЫЕ координаты разметки, без клипа: сравниваем их с
  // границами .shtorka (её собственный overflow-y:auto) в (д) ниже — тот же
  // приём, что раньше сравнивал выбранный узел с .lenta, пока список жил
  // в главной колонке.
  const stroki = uzly ? [...uzly.querySelectorAll('.uzel')]
    .map((el) => ({el, rr: el.getBoundingClientRect()}))
    .filter(({rr}) => rr.width > 0 || rr.height > 0)
    .map(({el, rr}) => ({
      imya: (el.querySelector('.imya') || el).textContent.trim().slice(0, 40),
      top: rr.top, bottom: rr.bottom,
      vybran: el.classList.contains('vybran'),
      galochka: !!el.querySelector('.galochka'),
    })) : [];
  return {
    fonVidno: !!(fon && !fon.hidden),
    kolichestvoBar: document.querySelectorAll('#shtorka-fon .rezhim-perekl').length,
    est_bar: !!barRect,
    avtoText: avto ? avto.textContent.trim() : null,
    ruchnoyText: ruchnoy ? ruchnoy.textContent.trim() : null,
    perekryto: perekryto.slice(0, 4),
    uzlyVidno: !!(uzly && !uzly.hidden && r(uzly)),
    shtorkaRect,
    stroki,
  };
}"""


def proverit_kartu_i_shtorku(str_, imya_sceny, avtorezhim_vklyuchen_ozhidaem):
    """(з) Карточка «Выход» + шторка выбора (29.08). Полоса режима и весь
    список узлов, раньше висевшие на главном экране всегда (третье их место
    после «Настройки» и главного экрана, см. историю в стенде выше), теперь
    живут в шторке, открывающейся кликом по карточке — заказ Вовы 27.08:
    «должно быть тупо выбор авто режим который сам все определяет, или
    ручной и там сам выбираешь», и 29.08: «ручной режим если тыкаешь то там
    выбор сделан криво» (подсветка выбранного узла).

    (з0) карточка `.karta-vyhod` — ровно одна, с непустыми title/subtitle
        и шевроном; шторка ДО клика закрыта.
    (з1) клик по карточке открывает шторку: внутри неё ровно одна полоса
        `.rezhim-perekl` с дословными подписями «Автоматически»/«Вручную»,
        без перекрытий, и виден список `#uzly` — эталон телефона,
        HomeScreen.kt 413: список виден в ОБОИХ режимах, не только в ручном.
    (д)  выбранный узел не обрезан границей `.shtorka` (retarget прежней (д),
        измерявшей то же самое против `.lenta`, пока список жил на главном).
    (з2) в авторежиме подсветку (.vybran) не несёт НИ ОДИН узел — эталон
        HomeScreen.kt 424 (selected = !auto.auto && ch.selected); в ручном —
        выбранный узел несёт подсветку И галочку ✓ (ExitRow, 528-529).
    (з3) Esc закрывает шторку, и на вкладке «Настройки» полосы режима нет.
    """
    bedy = []
    d = str_.evaluate(KARTA_GLAVNAYA_JS)
    if not d["glavnyyEkranViden"]:
        # Сцена «1_kod» не рисует главный экран вовсе — нечего мерить (тот же
        # приём, что и в proverit_glavnyy_ekran).
        return bedy
    if d["kolichestvoKart"] != 1 or not d["kartaVidna"]:
        bedy.append(f"{imya_sceny}: (з0) карточка «Выход» `.karta-vyhod` не видна на вкладке "
                    f"«Сеть» (найдено {d['kolichestvoKart']})")
    else:
        if not d["titleText"]:
            bedy.append(f"{imya_sceny}: (з0) карточка «Выход» без текста состояния (title пуст)")
        if not d["subtitleText"]:
            bedy.append(f"{imya_sceny}: (з0) карточка «Выход» без подписи (subtitle пуст)")
        if not d["shevronEst"]:
            bedy.append(f"{imya_sceny}: (з0) у карточки «Выход» нет шеврона «›»")
    if d["shtorkaVidnaDoKlika"]:
        bedy.append(f"{imya_sceny}: (з0) шторка выбора открыта БЕЗ клика по карточке")
    if bedy:
        return bedy  # без рабочей карточки открывать шторку нечем

    str_.click("#karta-vyhod")
    str_.wait_for_timeout(200)
    ds = str_.evaluate(SHTORKA_OTKRYTA_JS)
    if not ds["fonVidno"]:
        bedy.append(f"{imya_sceny}: (з1) клик по карточке «Выход» не открыл шторку")
        return bedy
    if not ds["est_bar"]:
        bedy.append(f"{imya_sceny}: (з1) в открытой шторке нет полосы выбора режима `.rezhim-perekl`")
    else:
        if ds["kolichestvoBar"] != 1:
            bedy.append(f"{imya_sceny}: (з1) полос выбора режима в шторке {ds['kolichestvoBar']}, "
                        "а нужна РОВНО одна")
        if ds["avtoText"] != "Автоматически":
            bedy.append(f"{imya_sceny}: (з1) подпись половины «{ds['avtoText']}», "
                        "а нужна дословно «Автоматически»")
        if ds["ruchnoyText"] != "Вручную":
            bedy.append(f"{imya_sceny}: (з1) подпись половины «{ds['ruchnoyText']}», "
                        "а нужна дословно «Вручную»")
        if ds["perekryto"]:
            chto = ", ".join(f"«{z['tekst']}» на {z['dy']}px" for z in ds["perekryto"])
            bedy.append(f"{imya_sceny}: (з1) полоса режима перекрывается с текстом шторки: {chto}")
    if not ds["uzlyVidno"]:
        bedy.append(f"{imya_sceny}: (з1) список узлов `#uzly` не виден в открытой шторке "
                    "(эталон телефона — HomeScreen.kt 413, список виден в обоих режимах)")

    shtorka_r = ds["shtorkaRect"]
    if shtorka_r:
        for u in ds["stroki"]:
            if not u["vybran"]:
                continue
            if u["top"] < shtorka_r["top"] - 1 or u["bottom"] > shtorka_r["bottom"] + 1:
                bedy.append(f"{imya_sceny}: (д) выбранный узел «{u['imya']}» обрезан шторкой "
                            f"(узел {u['top']:.0f}…{u['bottom']:.0f}, "
                            f"шторка {shtorka_r['top']:.0f}…{shtorka_r['bottom']:.0f})")

    vybranno = [u for u in ds["stroki"] if u["vybran"]]
    if avtorezhim_vklyuchen_ozhidaem:
        if vybranno:
            imena = ", ".join(f"«{u['imya']}»" for u in vybranno)
            bedy.append(f"{imya_sceny}: (з2) авторежим включён, а подсвечен узел {imena} — "
                        "эталон телефона (HomeScreen.kt 424): в авто не подсвечен НИКТО")
    else:
        if ds["stroki"] and len(vybranno) != 1:
            bedy.append(f"{imya_sceny}: (з2) ручной режим, узлов в шторке {len(ds['stroki'])}, "
                        f"а подсвеченных {len(vybranno)} — должен быть ровно 1")
        for u in vybranno:
            if not u["galochka"]:
                bedy.append(f"{imya_sceny}: (з2) выбранный узел «{u['imya']}» без галочки ✓ "
                            "(эталон телефона — ExitRow, HomeScreen.kt 528-529)")

    # Закрываем шторку тем же Esc, что обещан человеку (index.html:
    # document.addEventListener('keydown', ...)) — и той же дверью придётся
    # уйти, чтобы кликнуть по вкладке «Настройки»: .shtorka-fon перекрывает
    # собой всё окно, включая панель вкладок (z-index 5 против 3 у .taby).
    str_.keyboard.press("Escape")
    str_.wait_for_timeout(200)
    zakrylos = str_.evaluate(
        "() => { const f = document.getElementById('shtorka-fon'); return !!(f && f.hidden); }")
    if not zakrylos:
        bedy.append(f"{imya_sceny}: (з3) Esc не закрыл шторку — панель вкладок недоступна, "
                    "дальше не проверяю (клик по ней завис бы на перекрытом фоне)")
        return bedy

    str_.click("#vkladka-nastroyki")
    str_.wait_for_timeout(200)
    dn = str_.evaluate(REZHIM_NASTROYKI_JS)
    if dn["barVNastroykah"] != 0:
        bedy.append(f"{imya_sceny}: (з3) полоса выбора режима видна на вкладке «Настройки» "
                    f"({dn['barVNastroykah']}) — она обязана жить в шторке «Сети»")
    # Возвращаем сцену на вкладку «Сеть» — иначе следующая проверка в этой же
    # sтранице (proverit_gradacii_signala на 24_uzly_vse_gradacii) искала бы
    # #karta-vyhod на скрытой сейчас вкладке и молчала бы вместо суждения.
    str_.click("#vkladka-set")
    str_.wait_for_timeout(200)
    return bedy




SIGNAL_JS = """() => {
  const sostoyaniya = ["bystro", "sredne", "medlenno", "mertv"];
  const kakoe = (el) => sostoyaniya.find((s) => el.classList.contains(s)) || "bez_zamera";
  const otpechatki = {};
  for (const el of document.querySelectorAll(".uzel .signal")) {
    const st = getComputedStyle(el);
    otpechatki[kakoe(el)] = [st.backgroundColor, st.boxShadow,
                             st.borderStyle, st.borderWidth, st.borderColor].join("|");
  }
  return otpechatki;
}"""


def proverit_gradacii_signala(str_, imya_sceny):
    """Пять состояний .uzel .signal обязаны различаться на глаз (д. выше).

    Судим по фактическому getComputedStyle, а не по тексту правила CSS: цвет
    приходит из var(--zhdem) и т.п., и «средне» с «медленно» можно свести к
    одному виду, ни разу не тронув слово medlenno в самом правиле — ровно так
    беда 25.08 и прошла бы мимо текстового сравнения.

    Список узлов с 29.08 живёт в шторке (.shtorka, закрыта по умолчанию) —
    открываем её тем же кликом, что и человек, иначе querySelectorAll ниже
    смотрел бы в пустоту.
    """
    str_.click("#karta-vyhod")
    str_.wait_for_timeout(200)
    otp = str_.evaluate(SIGNAL_JS)
    zhdem = ["bystro", "sredne", "medlenno", "mertv", "bez_zamera"]
    otsutstvuet = [s for s in zhdem if s not in otp]
    if otsutstvuet:
        return [f"{imya_sceny}: (е) сцена не показала состояния {otsutstvuet} — "
                "щупу нечем судить попарную разницу"]
    bedy = []
    for i, a in enumerate(zhdem):
        for b in zhdem[i + 1:]:
            if otp[a] == otp[b]:
                bedy.append(f"{imya_sceny}: (е) состояния «{a}» и «{b}» сигнала неразличимы "
                            f"на экране (одинаковый вид: {otp[a]})")
    return bedy


ULIKA_JS = """() => {
  const r = (el) => { if (!el) return null; const q = el.getBoundingClientRect();
    return {top: +q.top.toFixed(1), bottom: +q.bottom.toFixed(1),
            left: +q.left.toFixed(1), right: +q.right.toFixed(1)}; };
  const uz = document.getElementById('uzly');
  const lenta = document.getElementById('lenta');
  const korobka = (el) => el ? {rect: r(el), scrollTop: el.scrollTop,
      scrollHeight: el.scrollHeight, clientHeight: el.clientHeight,
      styleHeight: el.style.height || '(не задана)',
      overflowY: getComputedStyle(el).overflowY} : null;
  return {
    okno: {w: window.innerWidth, h: window.innerHeight},
    readyState: document.readyState,
    shrifty: document.fonts ? document.fonts.status : '(нет API)',
    listov_css: [...document.styleSheets].length,
    uzly: korobka(uz),
    lenta: korobka(lenta),
    // Всё, что стоит НАД списком в той же колонке: если список уехал вниз,
    // виноват кто-то из них, и высота виновника видна прямо здесь.
    nad_spiskom: uz && uz.parentElement
      ? [...uz.parentElement.children].filter((e) => e !== uz).map((e) => ({
          tag: e.tagName + (e.id ? '#' + e.id : '') + (e.className ? '.' + String(e.className).split(' ').join('.') : ''),
          rect: r(e), tekst: (e.textContent || '').trim().slice(0, 60)}))
      : [],
    stroki: uz ? [...uz.querySelectorAll('.uzel')].map((e) => ({
        imya: (e.textContent || '').trim().slice(0, 30), vybran: e.classList.contains('vybran'),
        rect: r(e), display: getComputedStyle(e).display})) : [],
  };
}"""


def ulika(str_, imya_sceny, bedy):
    """Красный обязан оставить УЛИКУ, иначе следующий разбор — гадание.

    27.08 `oblik_geometriya.py` покраснел ВНУТРИ приёмки и был зелёным в
    одиночку три раза подряд («выбранный узел „Нидерланды 2" обрезан лентой,
    узел 643…687, лента 56…594»). Одних этих двух чисел мало, чтобы отличить
    брак вёрстки от недосмотренного кадра: неизвестно, что стояло НАД списком,
    какой у коробки был scrollTop и доехали ли шрифты. Восстановить это потом
    нечем — сцена живёт только внутри прогона. Поэтому каждый красный кладёт
    рядом снимок и полный слепок геометрии.

    Второй замер через 2с НЕ меняет вердикт (иначе прибор ослепнет ровно на
    том браке, который проявляется медленно) — он лишь пишет в улику
    `ustoyalos`: беда та же и через две секунды или рассосалась сама.
    """
    kuda = KOREN / ".stend" / "geom_ulika"
    kuda.mkdir(parents=True, exist_ok=True)
    try:
        d = str_.evaluate(ULIKA_JS)
        str_.screenshot(path=str(kuda / f"{imya_sceny}.png"))
        str_.wait_for_timeout(2000)
        povtor = proverit_glavnyy_ekran(str_, imya_sceny)
        d["bedy"] = bedy
        d["bedy_cherez_2s"] = povtor
        d["ustoyalos"] = sorted(povtor) == sorted(bedy)
        (kuda / f"{imya_sceny}.json").write_text(
            json.dumps(d, ensure_ascii=False, indent=2), encoding="utf-8")
        print(f"      🔎 улика: {kuda}/{imya_sceny}.json + .png; "
              f"через 2с бед {len(povtor)} (было {len(bedy)}) — "
              f"{'та же беда, кадр ни при чём' if d['ustoyalos'] else 'ИНАЯ картина: кадр был недосмотрен'}")
    except Exception as e:  # улика — подспорье, а не приговор: её отказ не красит стенд
        print(f"      ⚠ улику снять не удалось: {e}")


def zamerit():
    vse_bedy = []
    with socketserver.TCPServer(("127.0.0.1", 0), Ruchki) as srv:
        port = srv.server_address[1]
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        from playwright.sync_api import sync_playwright
        with sync_playwright() as p:
            br = p.chromium.launch()
            # ВСЕ сцены, а не одна. 4_rabotaet — сцена, на снимке которой беды
            # были поставлены диагнозом (24.08), и первая версия забора мерила
            # только её. Тем же вечером глаза нашли ту же беду (г) на
            # 11_beda_port и 12_beda_seti — забор при этом печатал «геометрия в
            # порядке»: он судил по одной сцене, а вывод делал про экран. Вход
            # прибора обязан быть шире одного примера, иначе зелёный врёт про
            # всё, чего прибор не смотрел. Сцены без круга и без панели
            # (1_kod) отсеиваются сами: каждая проверка ниже требует своих
            # элементов и молчит, если их на сцене нет.
            for imya_sceny in SCENY:
                sostoyanie["tek"] = SCENY[imya_sceny]
                str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
                str_.goto(f"http://127.0.0.1:{port}/index.html")
                str_.wait_for_timeout(700)
                bedy = proverit_glavnyy_ekran(str_, imya_sceny)
                bedy += proverit_krug_os_x(str_, imya_sceny)
                bedy += proverit_kartu_i_shtorku(
                    str_, imya_sceny, bool(SCENY[imya_sceny].get("avtorezhim_vklyuchen")))
                if imya_sceny == "24_uzly_vse_gradacii":
                    bedy += proverit_gradacii_signala(str_, imya_sceny)
                znak = "🔴" if bedy else "🟢"
                print(f"  {znak} {imya_sceny}")
                for b in bedy:
                    print(f"      {b}")
                if bedy:
                    ulika(str_, imya_sceny, bedy)
                vse_bedy.extend(bedy)
                str_.close()
            br.close()
        srv.shutdown()
    return vse_bedy


if __name__ == "__main__":
    bedy = zamerit()
    if bedy:
        print(f"\nКРАСНО: {len(bedy)} бед геометрии главного экрана.")
        sys.exit(1)
    print("\nГеометрия главного экрана в порядке.")
