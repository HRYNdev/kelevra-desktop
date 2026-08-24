#!/usr/bin/env python3
"""Забор ГЕОМЕТРИИ главного экрана — четыре беды вёрстки, найденные на снимке
`4_rabotaet.png` (24.08), не ловятся ни одним щупом oblik_snimok.py: те смотрят
на достижимость/переполнение/обрезку/жаргон, а не на то, КАК куски экрана
читаются друг относительно друга. Мерим через getBoundingClientRect() на
реально отрендеренной сцене — тем же playwright и тем же сервером-заглушкой,
что и oblik_snimok.py (импортируем его, а не дублируем HTTP-обвязку).

Пороги (заданы человеком, не подкручены под результат):
  (а) низ круга → верх подсказки-подсказки под ним          ≤ 24px
  (б) круг→подсказка СТРОГО МЕНЬШЕ подсказка→строка ключа   (подпись обязана
      принадлежать кругу визуально, а не строке ключа доступа под ней)
  (в) горизонтальный зазор между соседними кусками строки
      «Ключ доступа: ИМЯ … действует до ДАТЫ»                ≤ 16px
  (г) ни один элемент списка узлов не перекрыт ЧАСТИЧНО панелью вкладок:
      не должно существовать el, у которого el.top < tabbar.top < el.bottom
      (полностью скрытые за краем скроллящегося контейнера — норма)

    python3 stend/oblik_geometriya.py
"""
import socketserver, sys, threading
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from oblik_snimok import Ruchki, SCENY, sostoyanie, SHIRINA, VYSOTA  # noqa: E402

GEOMETRIYA_JS = """() => {
  const r = (el) => el ? el.getBoundingClientRect() : null;
  const krug = r(document.querySelector('.krug-fon'));
  const podskazka = r(document.getElementById('podskazka'));
  const podpiska = r(document.getElementById('podpiska'));
  const imya = r(document.getElementById('podpiska-imya'));
  const srok = r(document.getElementById('podpiska-srok'));
  const vkladki = r(document.getElementById('vkladki'));
  // getBoundingClientRect не знает про overflow предка: узел списка, чей
  // низ по разметке уходит НИЖЕ края скроллящейся .lenta, физически не
  // рисуется там (.lenta режет его как overflow), но rect всё равно вернёт
  // сырые координаты — «полностью скрытый за границей контейнера», а не
  // «торчит из-под панели». Поэтому низ узла обрезаем границей САМОЙ .lenta
  // ПЕРЕД сравнением с панелью — иначе щуп путает настоящий торчащий край
  // с куском, которого на экране физически нет.
  const lenta = document.getElementById('lenta').getBoundingClientRect();
  const uzly = [...document.querySelectorAll('#uzly .uzel')].map((el) => {
    const rr = el.getBoundingClientRect();
    return {imya: (el.querySelector('.imya') || el).textContent.trim().slice(0, 40),
            top: rr.top, bottom: Math.min(rr.bottom, lenta.bottom)};
  });
  return {krug, podskazka, podpiska, imya, srok, vkladki, uzly};
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

    vkladki = d["vkladki"]
    if vkladki and vkladki["height"] > 0:
        for u in d["uzly"]:
            if u["top"] < vkladki["top"] < u["bottom"]:
                bedy.append(f"{imya_sceny}: (г) узел «{u['imya']}» пересекает панель вкладок "
                            f"(узел {u['top']:.0f}…{u['bottom']:.0f}, панель.top={vkladki['top']:.0f})")

    return bedy


def zamerit():
    vse_bedy = []
    with socketserver.TCPServer(("127.0.0.1", 0), Ruchki) as srv:
        port = srv.server_address[1]
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        from playwright.sync_api import sync_playwright
        with sync_playwright() as p:
            br = p.chromium.launch()
            # 4_rabotaet — ровно сцена диагноза (снимок 4_rabotaet.png, 24.08):
            # подписка видна (mozhno_tun), список узлов не пуст и содержит
            # «Нидерланды 2» — ту самую строку, что упирается в панель. Гейт
            # держит эту сцену; 14/15 (тот же главный экран, другое состояние)
            # смотрятся глазами на снимках stend/oblik_snimok.py (шаг 3), не
            # этим забором — их геометрия отдельным диагнозом не ставилась.
            for imya_sceny in ("4_rabotaet",):
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
