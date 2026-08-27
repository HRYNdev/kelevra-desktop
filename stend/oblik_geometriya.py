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
  (д) ВЫБРАННЫЙ узел списка (класс .vybran) обязан помещаться в ленте
      целиком: lenta.top <= узел.top и узел.bottom <= lenta.bottom (допуск
      1px). Обрезанная НЕвыбранная строка — нормальный признак прокрутки,
      беды тут нет; но если обрезан именно текущий выбор — человек не видит,
      что у него выбрано (снимки 11_beda_port.png/12_beda_seti.png, 24.08:
      «Нидерланды 2» обрезана ровно посередине текста).
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
      текст. Обе беды названы человеком, а не выведены мной: хозяин 27.08 10:29
      «он *** всё ещё стоит *** не по центру а слева», «закрывает обзор
      на надписи», «ездит *** квадратом». Прежняя (ж) мерила уезд за
      ВЕРХНИЙ край при прокрутке — мой неверный разбор его слов 25.08.

    python3 stend/oblik_geometriya.py
"""
import socketserver, sys, threading
from pathlib import Path

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
  // top/bottom — СЫРЫЕ координаты разметки, без клипа по .lenta: раньше низ
  // узла обрезали границей ленты ДО сравнения, и (д) ниже проверяет ровно
  // то, что клип прятал — вылезает ли выбранный узел за реальные границы
  // скроллящегося контейнера (overflow режет его физически, но rect об этом
  // молчит).
  const uzly = [...document.querySelectorAll('#uzly .uzel')]
    .map((el) => ({el, rr: el.getBoundingClientRect()}))
    .filter(({rr}) => rr.width > 0 || rr.height > 0)
    .map(({el, rr}) => ({
      imya: (el.querySelector('.imya') || el).textContent.trim().slice(0, 40),
      top: rr.top, bottom: rr.bottom,
      vybran: el.classList.contains('vybran'),
    }));
  return {krug, podskazka, podpiska, imya, srok, vkladki, lenta, uzly};
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

    if lenta:
        for u in d["uzly"]:
            if not u["vybran"]:
                continue
            if u["top"] < lenta["top"] - 1 or u["bottom"] > lenta["bottom"] + 1:
                bedy.append(f"{imya_sceny}: (д) выбранный узел «{u['imya']}» обрезан лентой "
                            f"(узел {u['top']:.0f}…{u['bottom']:.0f}, лента {lenta['top']:.0f}…{lenta['bottom']:.0f})")

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
    был мой разбор слов хозяина 25.08 «круг куда то съехал», и разобрал я их
    неверно. Его же слова 27.08 10:29, дословно: «он *** всё ещё стоит ***
    не по центру а слева»; «при скролле ещё и *** то ездит дак ещё и криво
    ездит так как закрывает обзор на надписи, а ещё он ездит *** квадратом».
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
    """
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
                if imya_sceny == "24_uzly_vse_gradacii":
                    bedy += proverit_gradacii_signala(str_, imya_sceny)
                znak = "🔴" if bedy else "🟢"
                print(f"  {znak} {imya_sceny}")
                for b in bedy:
                    print(f"      {b}")
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
