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
