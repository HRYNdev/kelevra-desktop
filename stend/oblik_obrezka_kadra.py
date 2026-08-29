#!/usr/bin/env python3
"""Забор ПЕРВОГО КАДРА: ловит класс беды «элемент обрезан пополам верхней/
нижней границей видимой области, а прокрутки ещё не было». Диагноз
следователя (27.08) на главном экране в состоянии «rabotaet + mozhno_tun +
2 узла»: второй узел «Комната» занимал 572.7..617.0, лента кончалась на
lenta.bottom=594 — из 44.3px высоты узла видно было 21.3px, ни целиком, ни
скрыт: висящий обрубок текста и кружка сигнала. Ни один из существующих
щупов этот класс не ловит: oblik_snimok.py::proverit_geometriyu меряет
ДОСТИЖИМОСТЬ ПОСЛЕ прокрутки (scrollIntoView), а oblik_geometriya.py (д)
проверяет только ВЫБРАННЫЙ узел (.vybran), а не любой значимый элемент, и
тоже не различает «обрезан» от «просто ниже кадра» так строго, как нужно
здесь. Этот забор смотрит РОВНО на первый кадр — до единой прокрутки — и
судит только это.

ПРАВИЛО (формулировка человека, дословно, это и есть договор щупа):
  «На первом кадре (без единой прокрутки) ни один значимый интерактивный
  элемент не должен быть обрезан нижней/верхней границей видимой области:
  либо он виден целиком, либо не виден вовсе.»

Значимые элементы — селекторы заданы ОДНИМ местом ниже, ЗНАЧИМЫЕ_SELEKTORY:
узлы списка (.uzel), любые кнопки (button — тот же тег, что и .uzel: узел
списка сам является <button>, index.html:1029), переключатели (role=switch —
тумблер автозапуска). Дубликаты (.uzel одновременно попадает и под «button»)
схлопываются через JS Set по самому DOM-узлу, а не по строке селектора.

Видимая область элемента — не окно целиком, а ближайший предок, который его
РЕАЛЬНО клипает (getComputedStyle().overflowY в hidden/auto/scroll),
пересечённый с окном. У списка узлов это `#lenta` (overflow-y:auto,
index.html) — панель вкладок `#vkladki` внизу не предок ленты, а сосед, её
эта проверка не касается. Элемент вне такого предка меряется против самого
окна (0..VYSOTA) — так и кнопки шапки/панели вкладок остаются под приглядом.

Критерий обрезки: элемент ПЕРЕСЕКАЕТ границу видимой области, но не
помещается в нём целиком (допуск 1px на округление flexbox/circle — не
больше, иначе допуск спрячет ровно ту беду, ради которой щуп написан).
Целиком ЗА границей (весь ниже/выше, до прокрутки не долистать) — это НЕ
беда: список длиннее окна по построению, это и есть смысл прокрутки.

Если ни один значимый элемент не измерен (селекторы никого не нашли, сцена
не поднялась, видимая область не измерилась — например, при ошибке верстки,
из-за которой #lenta пропадает) — это КРАСНЫЙ сам по себе: щуп не имеет
права молчать зелёным, если ему нечего было смотреть.

    python3 stend/oblik_obrezka_kadra.py
"""
import socketserver, sys, threading
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from oblik_snimok import Ruchki, SCENY, sostoyanie, SHIRINA, VYSOTA, BAZA, ZAMETKI  # noqa: E402

# Одно место для набора значимых элементов — без него набор со временем
# разъедется по копипастам щупа (см. класс беды из памяти: «повторить
# копипастой» дороже, чем сослаться на одно определение).
ZNACHIMYE_SELEKTORY = ["#uzly .uzel", "button", "[role='switch']"]

# Допуск на округление разметки — не подкручивать выше 1px, иначе спрячет
# ровно 21px обрубленной «Комнаты», ради которой щуп и писан.
DOPUSK_PX = 1

OBREZKA_JS = """(selektory) => {
  function skryt(el) {
    if (el.hidden) return true;
    const st = getComputedStyle(el);
    if (st.display === "none" || st.visibility === "hidden") return true;
    for (let p = el.parentElement; p; p = p.parentElement) {
      if (p.hidden) return true;
      const ps = getComputedStyle(p);
      if (ps.display === "none" || ps.visibility === "hidden") return true;
    }
    return false;
  }
  function klichka(el) {
    const tekst = (el.id || el.textContent || "").trim().replace(/\\s+/g, " ").slice(0, 40);
    const opora = tekst || (typeof el.className === "string" && el.className.trim()) || el.tagName.toLowerCase();
    return `${el.tagName.toLowerCase()}${el.id ? "#" + el.id : ""} «${opora}»`;
  }
  function vidimayaObl(el) {
    // Границы видимой области = окно, пересечённое со всеми предками,
    // которые элемент РЕАЛЬНО клипают (overflow-y auto/hidden/scroll) —
    // тем же приёмом, каким это решает сам браузер. #lenta клипает список
    // узлов именно так; панель вкладок `#vkladki` — не предок, а сосед,
    // на клип списка она никак не участвует (диагноз 24.08 в
    // oblik_geometriya.py: PORCHA_CSS по #vkladki не двигает lenta.bottom).
    let top = 0, bottom = window.innerHeight;
    for (let p = el.parentElement; p; p = p.parentElement) {
      const ps = getComputedStyle(p);
      if (["hidden", "auto", "scroll"].includes(ps.overflowY)) {
        const r = p.getBoundingClientRect();
        top = Math.max(top, r.top);
        bottom = Math.min(bottom, r.bottom);
      }
    }
    return {top, bottom};
  }
  const naideno = new Set();
  for (const sel of selektory) document.querySelectorAll(sel).forEach((el) => naideno.add(el));
  const rezultat = [];
  for (const el of naideno) {
    if (skryt(el)) continue;
    const r = el.getBoundingClientRect();
    if (r.width === 0 && r.height === 0) continue;
    const obl = vidimayaObl(el);
    rezultat.push({imya: klichka(el), top: r.top, bottom: r.bottom,
                    oblTop: obl.top, oblBottom: obl.bottom});
  }
  return rezultat;
}"""


def proverit_obrezku_kadra(str_, imya_sceny):
    """Список бед сцены + сколько значимых элементов реально измерено.

    Возвращает (bedy, provereno) — второе число нужно снаружи, чтобы забор
    мог сказать «0 проверок = красный», а не молчать зелёным на пустом входе.
    """
    elementy = str_.evaluate(OBREZKA_JS, ZNACHIMYE_SELEKTORY)
    bedy, provereno = [], 0
    for e in elementy:
        top, bottom = e["top"], e["bottom"]
        obl_top, obl_bottom = e["oblTop"], e["oblBottom"]
        if obl_bottom - obl_top < 1:
            continue  # видимая область не измерилась (вырожденный rect) — не считаем
        provereno += 1
        if top < obl_bottom - DOPUSK_PX and bottom > obl_bottom + DOPUSK_PX:
            vidno = obl_bottom - top
            bedy.append(f"{imya_sceny}: {e['imya']} обрезан НИЖНЕЙ границей области "
                        f"(элемент {top:.1f}..{bottom:.1f}, граница {obl_bottom:.1f}) — "
                        f"видно {vidno:.1f}px из {bottom - top:.1f}px")
        if bottom > obl_top + DOPUSK_PX and top < obl_top - DOPUSK_PX:
            vidno = bottom - obl_top
            bedy.append(f"{imya_sceny}: {e['imya']} обрезан ВЕРХНЕЙ границей области "
                        f"(элемент {top:.1f}..{bottom:.1f}, граница {obl_top:.1f}) — "
                        f"видно {vidno:.1f}px из {bottom - top:.1f}px")
    return bedy, provereno


# Минимум сцен, заданный человеком буквально: «главный экран с 2 узлами и
# mozhno_tun» — ни одна сцена в SCENY (oblik_snimok.py) не несёт ровно 2
# узла (там их всегда 4 или 5, см. UZLY/UZLY_VSE_GRADACII), поэтому забор
# кладёт СВОЮ сцену рядом с чужими, тем же способом, каким описаны все
# остальные сцены SCENY (dict(BAZA, ...)) — не пишем новый перебор, только
# расширяем вход старому. Имена узлов — «Нидерланды»/«Комната», как в
# реальном конфиге следователя (internal/yadro/uzly_test.go), не выдуманные.
SCENA_DVA_UZLA = dict(
    BAZA, sost="rabotaet", pid="8124", rezhim="proksi", mozhno_tun=True,
    zametka=ZAMETKI["ZametkaBezPrav"],
    uzly={"gruppy": [{
        "imya": "Соединение", "sam": False, "seychas": "Комната",
        "uzly": [{"imya": "Нидерланды", "zaderzhka": 64}, {"imya": "Комната", "zaderzhka": 12}],
    }]},
)

SCENY_DLYA_ZABORA = dict(SCENY)
SCENY_DLYA_ZABORA["25_dva_uzla_mozhno_tun"] = SCENA_DVA_UZLA

# Порча для самоконтроля — ДИАГНОЗ 27.08 (следователь, playwright-замер):
# `.lenta { max-height: 400px !important; }` на 4_rabotaet НЕ режет ничего,
# и дело не в тайминге (проверено паузами 200/750/900/2000/4100мс после
# add_style_tag — рект #uzly не меняется ни на пиксель ни разу). Причина —
# арифметика: 400px обрезает `.lenta` настолько, что верх `#uzly` (459.75px
# сверху) целиком уезжает ЗА новую нижнюю границу `.lenta` (456px) — весь
# список узлов пропадает под границей ЦЕЛИКОМ, а не разрезается пополам, и
# «целиком за границей» — по договору щупа НЕ беда (см. шапку файла). Число
# 400 было подобрано на глаз под старую вёрстку и умерло молча, когда
# `#uzly` получил свой overflow-y:auto — это и есть тот самый класс беды,
# который просил не повторять человек: эмпирический порог хрупок к любой
# следующей правке пикселей.
#
# Порча ниже не подбирает число заранее, а вычисляет его из ЖИВОЙ геометрии
# сцены прямо перед порчей. ВТОРАЯ СМЕРТЬ ТОГО ЖЕ МЕСТА (29.08): порча целила
# в `#uzly` — контейнер списка узлов на главном экране. Список узлов переехал
# в шторку выбора выхода, на сцене 4_rabotaet `#uzly` теперь СКРЫТ, его rect =
# 0×0, и порча честно вычислялась в `max-height: 0.000px` — то есть портила
# невидимое и не резала ничего. Забор от этого не позеленел (контроль сказал
# «щуп мёртв»), но урок тот же, что 27.08: щуп нельзя привязывать к ИМЕНИ
# конкретного узла вёрстки, он переживёт переезд только если спросит у живой
# страницы, КТО её сейчас клипает.
#
# Поэтому порча больше не знает названий: она берёт клипающий контейнер
# первого кадра (`overflow-y: auto|scroll` — сегодня это `#tab-set`, до 29.08
# был `#uzly`, завтра будет что угодно), находит в нём ПОСЛЕДНИЙ значимый
# элемент, целиком лежащий в видимой области, и опускает нижнюю границу
# контейнера ровно на половину высоты этого элемента. Граница
# ГАРАНТИРОВАННО ложится в его середину — «целиком за границей» (не беда по
# договору щупа) исключено арифметикой, а не подбором числа.
KOEFF_SEREDINY_UZLA = 0.5  # доля высоты элемента, на которую режется контейнер

# Возвращает [selektor_konteynera, novaya_vysota] либо [null, prichina].
GEOMETRIYA_PORCHI_JS = """(selektory) => {
  const znach = [];
  for (const sel of selektory) document.querySelectorAll(sel).forEach((el) => {
    const r = el.getBoundingClientRect();
    if (r.height > 0 && r.width > 0) znach.push(el);
  });
  if (!znach.length) return [null, "на сцене нет ни одного видимого значимого элемента"];
  // Клипающий предок ищется у самих элементов, а не по имени в вёрстке.
  const klipery = new Map();
  for (const el of znach) {
    for (let p = el.parentElement; p; p = p.parentElement) {
      if (["auto", "scroll"].includes(getComputedStyle(p).overflowY)) {
        if (!klipery.has(p)) klipery.set(p, []);
        klipery.get(p).push(el);
        break;  // ближайший клипер — тот, чью границу мы и будем двигать
      }
    }
  }
  if (!klipery.size) return [null, "ни один значимый элемент не лежит в прокручиваемом контейнере"];
  // Самый населённый клипер = контейнер первого кадра.
  let kont = null, deti = [];
  for (const [p, xs] of klipery) if (xs.length > deti.length) { kont = p; deti = xs; }
  const kr = kont.getBoundingClientRect();
  // Последний ребёнок, целиком видимый внутри контейнера И окна: его-то
  // середину и должна пересечь новая граница.
  const niz = Math.min(kr.bottom, window.innerHeight);
  let cel = null;
  for (const el of deti) {
    const r = el.getBoundingClientRect();
    if (r.top >= kr.top && r.bottom <= niz && (!cel || r.bottom > cel.getBoundingClientRect().bottom)) cel = el;
  }
  if (!cel) return [null, "внутри контейнера нет ни одного целиком видимого значимого элемента"];
  const cr = cel.getBoundingClientRect();
  const selektor = kont.id ? "#" + kont.id
    : (typeof kont.className === "string" && kont.className.trim()
       ? "." + kont.className.trim().split(/\\s+/).join(".") : null);
  if (!selektor) return [null, "у клипающего контейнера нет ни id, ни класса — не за что зацепить порчу"];
  return [selektor, cr.bottom - kr.top - cr.height * """ + str(KOEFF_SEREDINY_UZLA) + """];
}"""


def kontrol_shchupa(br, port):
    """Щуп обязан покраснеть на заведомо испорченной странице. Промолчал —
    он мёртв, и зелень остальных сцен ничего не доказывает (тот же приём,
    что kontrol_shchupa в oblik_snimok.py и kontrol_geometrii рядом).

    Возвращает (bedy, porcha_css) — второе нужно снаружи, чтобы сообщение
    об ошибке всегда цитировало РЕАЛЬНО применённую порчу, а не текст,
    который может разойтись с кодом (как расходился раньше: сообщение
    говорило «520px», хотя порчено было уже 400px)."""
    sostoyanie["tek"] = SCENY["4_rabotaet"]
    str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
    str_.goto(f"http://127.0.0.1:{port}/index.html")
    str_.wait_for_timeout(700)
    selektor, znachenie = str_.evaluate(GEOMETRIYA_PORCHI_JS, ZNACHIMYE_SELEKTORY)
    if selektor is None:
        # Не измерили геометрию — это КРАСНЫЙ с внятной причиной, а не порча
        # в 0px, которая портит невидимое и выглядит как мёртвый щуп.
        str_.close()
        return [], f"ПОРЧУ НЕ ПОСТРОИТЬ: {znachenie}"
    porcha_css = f"{selektor} {{ max-height: {znachenie:.3f}px !important; overflow-y: auto !important; }}"
    str_.add_style_tag(content=porcha_css)
    str_.wait_for_timeout(200)
    bedy, _ = proverit_obrezku_kadra(str_, "контроль-обрезка")
    str_.close()
    return bedy, porcha_css


def zamerit():
    vse_bedy = []
    vsego_provereno = 0
    with socketserver.TCPServer(("127.0.0.1", 0), Ruchki) as srv:
        port = srv.server_address[1]
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        from playwright.sync_api import sync_playwright
        with sync_playwright() as p:
            br = p.chromium.launch()
            for imya_sceny in SCENY_DLYA_ZABORA:
                sostoyanie["tek"] = SCENY_DLYA_ZABORA[imya_sceny]
                str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
                str_.goto(f"http://127.0.0.1:{port}/index.html")
                str_.wait_for_timeout(700)
                # НИ ОДНОЙ прокрутки — щуп судит ровно первый кадр, тот, что
                # человек видит, ничего не тронув (scrollTop у #lenta и так
                # 0 при свежей загрузке страницы).
                bedy, provereno = proverit_obrezku_kadra(str_, imya_sceny)
                vsego_provereno += provereno
                znak = "🔴" if bedy else "🟢"
                print(f"  {znak} {imya_sceny} ({provereno} элементов)")
                for b in bedy:
                    print(f"      {b}")
                vse_bedy.extend(bedy)
                str_.close()
            kontrol, porcha_css = kontrol_shchupa(br, port)
            br.close()
        srv.shutdown()
    return vse_bedy, vsego_provereno, kontrol, porcha_css


if __name__ == "__main__":
    bedy, vsego_provereno, kontrol, porcha_css = zamerit()
    if not kontrol:
        print("\n🔴 ЩУП ОБРЕЗКИ ПЕРВОГО КАДРА МЁРТВ: на заведомо испорченной "
              f"странице (порча: {porcha_css!r}) он не нашёл ни одной обрезки. "
              "Зелень остальных сцен ничего не доказывает.")
        sys.exit(2)
    print(f"\n  🧪 контроль обрезки: щуп видит порчу ({len(kontrol)}):")
    for k in kontrol:
        print(f"      — {k}")
    if vsego_provereno == 0:
        print("\n🔴 КРАСНО: 0 выполненных проверок — селекторы никого не нашли "
              "или видимая область не измерилась, щупу нечем судить.")
        sys.exit(1)
    if bedy:
        print(f"\nКРАСНО: {len(bedy)} обрезанных на первом кадре элементов "
              f"из {vsego_provereno} проверенных.")
        sys.exit(1)
    print(f"\nОбрезки на первом кадре нет: {vsego_provereno} значимых элементов целы.")
