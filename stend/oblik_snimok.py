#!/usr/bin/env python3
"""Снимок облика приложения без Windows: поднимаем встроенный index.html,
подсовываем ему выдуманные ответы /api/* и фотографируем окно 420x660 —
ровно тот размер, что открывается у человека (cmd/kelevra/okno_windows.go).

Зачем: дизайн нельзя «проверить тестами», его надо УВИДЕТЬ. Смотреть на него
на живой машине Вовы — значит делать из него бета-тестера (его слова 20.08).

    python3 stend/oblik_snimok.py [папка-для-png]
"""
import http.server, json, os, re, socketserver, sys, threading, time
from pathlib import Path

KOREN = Path(__file__).resolve().parent.parent
# Папку облика можно подменить: так стенд гоняется против СТАРОЙ версии окна
# и доказывает, что щуп краснеет там, где беда была на самом деле.
OBLIK = Path(os.environ.get("KELEVRA_OBLIK") or (KOREN / "internal" / "sluzhba" / "oblik"))
VYHOD = Path(sys.argv[1]) if len(sys.argv) > 1 else KOREN / ".stend" / "oblik"

UZLY = {"gruppy": [{
    "imya": "Выбор узла", "sam": False, "seychas": "Нидерланды 2",
    "uzly": [
        {"imya": "Нидерланды 1", "zaderzhka": 78},
        {"imya": "Нидерланды 2", "zaderzhka": 64},
        {"imya": "Германия 1", "zaderzhka": 91},
        {"imya": "Финляндия 1", "beda": "нет ответа"},
    ]}]}

def zametki_iz_go():
    """Заметки окна — ИЗ konfig.go, а не выдуманные тут.

    20.08 стенд четырьмя сценами показывал «Ядро на месте, всё готово.» —
    строки, которой в продукте нет вообще, а настоящие («прокси-режим: в
    профиле нет туннеля») уезжали человеку, ни разу не попав под гейт. Стенд,
    сам сочиняющий вход, доказывает согласие себя с собой и больше ничего.
    """
    ishodnik = (KOREN / "internal" / "konfig" / "konfig.go").read_text(encoding="utf-8")
    nashli, imya, kuski, zhdem_prodolzhenie = {}, None, [], False
    for stroka in ishodnik.splitlines():
        golaya = stroka.strip()
        nachalo = re.match(r"^(Zametka\w+)\s*=\s*(.*)$", golaya)
        if nachalo:
            if imya:
                nashli[imya] = "".join(kuski)
            imya, kuski = nachalo.group(1), []
            golaya = nachalo.group(2)
        elif not zhdem_prodolzhenie:
            continue
        if imya:
            kuski += [k.replace('\\"', '"') for k in re.findall(r'"((?:[^"\\]|\\.)*)"', golaya)]
            zhdem_prodolzhenie = golaya.rstrip().endswith("+")
            if not zhdem_prodolzhenie:
                nashli[imya] = "".join(kuski)
                imya, kuski = None, []
    if len(nashli) < 4:
        raise SystemExit(f"🔴 в konfig.go нашлось лишь {len(nashli)} заметок: "
                         "стенд разучился их читать, гейт жаргона стал пустым")
    return nashli


ZAMETKI = zametki_iz_go()

# Слова, которых человек в окне видеть не должен: он не программист, и из
# «прокси-режим: в профиле нет туннеля» не следует ни что случилось, ни что
# нажать. Сырой лог живёт под «подробности» и в журнале — эти два места гейт
# не смотрит, они для меня. Гейт держит КЛАСС: новая жаргонная строка где
# угодно в окне покраснеет сама, без моей памятливости.
ZHARGON = ["ядро", "ядра", "ядру", "ядром", "туннел", "прокси-режим",
           "профил", "inbound", "sing-box", "fatal", "permission denied",
           "тэг", "конфиг"]

# «Срок на исходе» окно считает от СЕЙЧАС (dney <= 7), поэтому дату для этой
# сцены берём живую: жёстко вписанная 15.09 через месяц перестала бы быть
# исходом, и жёлтую подсветку снова не видел бы никто.
DO_SKORO = int(time.time()) + 3 * 86400

BAZA = {"versiya": "0.5.3", "kod_est": True, "sost": "stoit",
        "vniz_bayt": 0, "vverh_bayt": 0, "pid": 0,
        "imya": "Вова", "do_unix": 1789430400, "rezhim": "", "zametka": "",
        "mozhno_tun": False, "ruchnoy_proksi": False,
        "beda": "", "kachaem_yadro": False, "yadro_est": True}

SCENY = {
    "1_kod": dict(BAZA, kod_est=False),
    "2_otklyucheno": dict(BAZA),
    "3_podnimaem": dict(BAZA, sost="podnimaem", imya="Нидерланды 2"),
    "4_rabotaet": dict(BAZA, sost="rabotaet", pid="8124",
                       rezhim="proksi", vniz_bayt=418_365_440, vverh_bayt=21_495_808,
                       mozhno_tun=True, zametka=ZAMETKI["ZametkaBezPrav"]),
    "14_polnaya_zashchita": dict(BAZA, sost="rabotaet", pid="8124", rezhim="tunnel",
                                 vniz_bayt=1_073_741_824, vverh_bayt=52_428_800,
                                 zametka=ZAMETKI["ZametkaVes"]),
    "15_tolko_brauzery": dict(BAZA, sost="rabotaet", pid="8124", rezhim="proksi",
                              vniz_bayt=12_582_912, vverh_bayt=1_048_576,
                              zametka=ZAMETKI["ZametkaBezTunnelya"]),
    "7_ruchnoy_proksi": dict(BAZA, sost="rabotaet", pid="8124", rezhim="proksi",
                             vniz_bayt=5_242_880, vverh_bayt=524_288, ruchnoy_proksi=True,
                             zametka=ZAMETKI["ZametkaRuchnoyProksi"] % "127.0.0.1:2412"),
    "5_slomalos": dict(BAZA, sost="slomalos",
                       beda="ядро не ответило за 45 секунд: FATAL[0000] start service: "
                            "initialize inbound/tun[0]: configure tun interface: "
                            "permission denied"),
    "6_kachaem": dict(BAZA, kachaem_yadro=True, yadro_est=False),
    # Беды разных пород: окно обязано сказать, ЧТО случилось и ЧТО нажать,
    # а не пересказывать лог sing-box человеку, который не программист.
    "11_beda_port": dict(BAZA, sost="slomalos",
                         beda="ядро упало при старте: FATAL[0000] start service: "
                              "initialize inbound/mixed[0]: listen tcp 127.0.0.1:2412: "
                              "bind: Only one usage of each socket address is normally permitted."),
    "12_beda_seti": dict(BAZA, sost="slomalos",
                         beda="ядро не ответило за 45 секунд: ERROR[0007] dial tcp "
                              "185.204.1.14:443: i/o timeout"),
    "13_beda_konfig": dict(BAZA, sost="slomalos",
                                beda="ядро упало при старте: FATAL[0000] "
                                     "decode config at rule_set[2]: unexpected token '}'"),
    # Автозапуск живёт только на Windows, и облик прячет его ряд целиком по
    # avtozapusk_podderzhivaetsya. Стенд гоняется на Linux, поэтому без этих
    # сцен галочку не видит НИКТО — ни я, ни снимок: 20.08 она уехала в
    # релиз, ни разу не показавшись глазам. Поле подаём руками.
    "8_avtozapusk_vykl": dict(BAZA, avtozapusk_podderzhivaetsya=True,
                              avtozapusk_vklyuchen=False),
    "9_avtozapusk_vkl": dict(BAZA, avtozapusk_podderzhivaetsya=True,
                             avtozapusk_vklyuchen=True),
    "10_avtozapusk_ustarela": dict(BAZA, avtozapusk_podderzhivaetsya=True,
                                   avtozapusk_vklyuchen=True, avtozapusk_ustarela=True),
    # Крайние значения: имя ключа с сервера длиной не ко мне, и срок на исходе.
    # Кто-то другой с длинным именем — уже не Вова, и окно обязано остаться
    # читаемым: 420 px не растянешь.
    "16_dlinnoe_imya": dict(BAZA, sost="rabotaet", pid="8124", rezhim="tunnel",
                            imya="Владимир Александрович (семейный)",
                            zametka=ZAMETKI["ZametkaVes"]),
    "17_srok_na_ishode": dict(BAZA, sost="rabotaet", pid="8124", rezhim="tunnel",
                              do_unix=DO_SKORO, zametka=ZAMETKI["ZametkaVes"]),
}

sostoyanie = {"tek": SCENY["2_otklyucheno"]}


class Ruchki(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *a, **kw):
        super().__init__(*a, directory=str(OBLIK), **kw)

    def _json(self, telo):
        b = json.dumps(telo).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(b)))
        self.end_headers()
        self.wfile.write(b)

    def do_GET(self):
        if self.path.startswith("/api/sostoyanie"):
            return self._json(sostoyanie["tek"])
        if self.path.startswith("/api/uzly"):
            return self._json(UZLY if sostoyanie["tek"]["sost"] == "rabotaet"
                              else {"gruppy": []})
        if self.path.startswith("/api/zhurnal"):
            self.send_response(200)
            self.send_header("Content-Type", "text/plain; charset=utf-8")
            self.end_headers()
            self.wfile.write(("2026/08/20 12:14:02 ядро запущено: pid 8124\n"
                              "2026/08/20 12:14:03 ядро ответило: связь работает\n"
                              "2026/08/20 12:31:44 останавливаю ядро\n"
                              "2026/08/20 12:31:44 системный прокси снят\n").encode())
            return
        return super().do_GET()

    def log_message(self, *a):
        pass


SHIRINA, VYSOTA = 420, 660

# JS-щуп: до каждой ли видимой кнопки человек ДОБЕРЁТСЯ.
#
# Первая версия щупа (20.08) мерила только первый кадр и звала бедой всё, что
# ниже 660px. Внутри окна лежит своя прокрутка («.lenta», overflow-y:auto), и
# 10 из 10 её находок оказались обычной прокруткой — ложный красный на живом
# продукте. Поэтому мерим не «видно сразу», а «дотянуться можно»:
#   1. просим браузер докрутить контейнер до штуки — ровно то, что делает рукой
#      человек. Не докрутилось (штука всё равно за краем окна) — беда настоящая:
#      прокрутки нет или её не хватает;
#   2. смотрим elementFromPoint в центре штуки: если сверху лежит кто-то чужой
#      (зафиксированная снизу панель «.niz», модалка, наехавшая карточка) —
#      кнопка не тыкается, сколько её ни крути. Именно так ловится перекос
#      «padding-bottom у ленты — константа 98px, а высота панели зависит от
#      сцены»: панель выше константы — и последний ряд списка навсегда под ней.
DOSTUP_JS = """() => {
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
    if (!el) return "пусто";
    return el.id || (typeof el.className === "string" && el.className.trim())
           || el.tagName.toLowerCase();
  }
  const W = innerWidth, H = innerHeight;
  const bedy = [];
  const shtuki = [...document.querySelectorAll("button, [role=switch]")].filter((el) => !skryt(el));
  for (const el of shtuki) {
    let r = el.getBoundingClientRect();
    if (r.width === 0 && r.height === 0) continue;
    const imya = (el.id || el.textContent || "").trim().replace(/\\s+/g, " ").slice(0, 70);
    el.scrollIntoView({block: "center", inline: "center", behavior: "instant"});
    r = el.getBoundingClientRect();
    if (r.left < 0) bedy.push(`«${imya}» не достать: за левым краем на ${Math.round(-r.left)}px`);
    if (r.right > W) bedy.push(`«${imya}» не достать: за правым краем на ${Math.round(r.right - W)}px`);
    if (r.top < 0) bedy.push(`«${imya}» не достать: за верхним краем на ${Math.round(-r.top)}px`);
    if (r.bottom > H) bedy.push(`«${imya}» не достать: за нижним краем на ${Math.round(r.bottom - H)}px (прокрутка не спасает)`);
    const cx = Math.min(Math.max(r.left + r.width / 2, 0), W - 1);
    const cy = Math.min(Math.max(r.top + r.height / 2, 0), H - 1);
    const sverhu = document.elementFromPoint(cx, cy);
    if (!sverhu || (!el.contains(sverhu) && !sverhu.contains(el))) {
      bedy.push(`«${imya}» не тыкается: в его центре (${Math.round(cx)},${Math.round(cy)}) лежит «${klichka(sverhu)}»`);
    }
  }
  document.querySelectorAll("*").forEach((el) => { if (el.scrollTop) el.scrollTop = 0; });
  return bedy;
}"""


def proverit_geometriyu(str_, imya_sceny):
    """Список бед сцены: до чего человек не доберётся ни глазом, ни мышью."""
    return [f"{imya_sceny}: {b}" for b in str_.evaluate(DOSTUP_JS)]


# Порча для контроля: делаем нижнюю панель заведомо выше, чем зазор, который
# лента держит под неё (--nizhnyaya, константа). Это ровно та беда, ради
# которой щуп и живёт: панель переросла зазор — и последний ряд списка
# навсегда под ней, никакой прокруткой не достать.
PORCHA_CSS = ".niz { padding-bottom: 140px !important; }"


def kontrol_shchupa(br, port):
    """Щуп обязан покраснеть на испорченной странице. Промолчал — он мёртвый.

    Зелёный щуп ничего не значит сам по себе: 20.08 предыдущая версия этого
    же файла краснела 10 раз подряд на здоровом окне, а разбираться пришлось
    руками. Дешевле держать в стенде одну заведомо больную сцену.
    """
    sostoyanie["tek"] = SCENY["4_rabotaet"]
    str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
    str_.goto(f"http://127.0.0.1:{port}/index.html")
    str_.wait_for_timeout(700)
    str_.add_style_tag(content=PORCHA_CSS)
    str_.wait_for_timeout(200)
    bedy = proverit_geometriyu(str_, "контроль")
    str_.close()
    return bedy


# Перевод беды: сырой текст ядра → заголовок, который читает не программист.
# Держим таблицей, потому что осечка тут тихая: 20.08 «decode config at
# rule_set[2]» переводилось как «подключение зависло» — правдоподобно и мимо.
PEREVOD_ZHDEM = [
    ("ядро упало при старте: FATAL[0000] initialize inbound/tun[0]: "
     "configure tun interface: permission denied", "Windows не дал создать сетевой адаптер"),
    ("ядро не ответило за 45 секунд: FATAL configure tun: operation not permitted",
     "Windows не дал создать сетевой адаптер"),          # точная примета бьёт таймаут
    ("ядро упало при старте: listen tcp 127.0.0.1:2412: bind: address already in use",
     "Нужный порт занят другой программой"),
    ("ядро ответило 401", "Код доступа не подошёл"),
    ("ядро не ответило за 45 секунд: dial tcp 185.204.1.14:443: i/o timeout",
     "Не достучались до сервера"),
    ("ядро не найдено: C:\\\\Users\\\\Vova\\\\kelevra\\\\sing-box.exe", "На месте нет рабочего файла"),
    ("нет конфига: сначала введите код доступа", "Код доступа ещё не введён"),
    ("ядро упало при старте: decode config at rule_set[2]: unexpected token",
     "Настройки под твоим кодом доступа не читаются"),
    ("ядро не ответило за 45 секунд: ", "Подключение зависло"),
    ("ядро упало при старте: ", "Kelevra не смогла запуститься"),
    ("хтонь, какой ещё не бывало", "Связь не поднялась"),
]


# Таблица выше зовёт perevestiBedu() напрямую — она пройдёт, даже если окно
# перестанет её звать и снова начнёт печатать человеку сырой лог. Поэтому в
# живых сценах смотрим, что в блоке беды лежит ЗАГОЛОВОК, а сырое спрятано.
ZHDEM_V_OKNE = {
    "5_slomalos": "Windows не дал создать сетевой адаптер",
    "11_beda_port": "Нужный порт занят другой программой",
    "12_beda_seti": "Не достучались до сервера",
    "13_beda_konfig": "Настройки под твоим кодом доступа не читаются",
}


def proverit_okno_bedy(str_, imya_sceny, zhdem):
    vidno = str_.evaluate("""() => {
      const u = document.getElementById("beda-svyazi");
      const z = u.querySelector(".beda-zagolovok");
      const s = u.querySelector(".beda-syroe");
      return {zagolovok: z ? z.textContent : null,
              syroe_spryatano: s ? s.hidden : null,
              ves_tekst: u.textContent};
    }""")
    bedy = []
    if vidno["zagolovok"] != zhdem:
        bedy.append(f'{imya_sceny}: в окне заголовок беды «{vidno["zagolovok"]}», '
                    f'а ждали «{zhdem}» — окно печатает не то, что переводит')
    if vidno["syroe_spryatano"] is not True:
        bedy.append(f"{imya_sceny}: сырой лог ядра не спрятан под «подробности»")
    if "FATAL" in (vidno["ves_tekst"] or "") and vidno["syroe_spryatano"] is not True:
        bedy.append(f"{imya_sceny}: человеку видно сырое FATAL[...] из лога")
    return bedy


VIDIMYY_TEKST_JS = """() => {
  // Сырое — под «подробности» и в журнале: эти два места человек открывает
  // сам, зная, что там машинные буквы. Всё остальное на экране — его язык.
  const skryt = [...document.querySelectorAll("#zhurnal, .beda-syroe")];
  const bylo = skryt.map((e) => e.style.display);
  skryt.forEach((e) => { e.style.display = "none"; });
  const t = document.body.innerText;
  skryt.forEach((e, i) => { e.style.display = bylo[i]; });
  return t;
}"""


PERELIV_JS = """() => {
  // Текст, который шире своего блока и никуда не прокручивается, человек
  // просто не дочитает: адрес прокси, длинное имя ключа, имя узла.
  const bedy = [];
  for (const el of document.querySelectorAll("body *")) {
    if (!el.offsetParent && el !== document.body) continue;
    if (el.children.length) continue;                 // судим только листья
    const s = getComputedStyle(el);
    if (/auto|scroll/.test(s.overflowX)) continue;
    if (s.textOverflow === "ellipsis") continue;      // обрезано намеренно
    const podpis = (el.textContent || "").trim().slice(0, 34);
    if (!podpis) continue;
    if (el.scrollWidth > el.clientWidth + 1 && el.clientWidth > 0) {
      bedy.push(`«${podpis}…» не влез в свой блок (${el.scrollWidth} > ${el.clientWidth} px)`);
      continue;
    }
    // Главный случай: строка не переполняет СЕБЯ, она распирает родителя и
    // уезжает за край окна. Ровно так nowrap выносит длинное имя ключа за 420 px.
    const r = el.getBoundingClientRect();
    if (r.width > 0 && (r.right > window.innerWidth + 1 || r.left < -1)) {
      bedy.push(`«${podpis}…» уехал за край окна `
                + `(${Math.round(r.left)}…${Math.round(r.right)} при ширине ${window.innerWidth})`);
    }
  }
  return bedy;
}"""


def proverit_pereliv(str_, imya_sceny):
    return [f"{imya_sceny}: {b}" for b in str_.evaluate(PERELIV_JS)]


SOSTOYANIE_V_KADRE_JS = """() => {
  const el = document.getElementById("sostoyanie");
  if (!el.offsetParent) return null;   // скрыт (сцена ввода кода) — нечего мерить
  const lenta = document.getElementById("lenta");
  lenta.scrollTop = lenta.scrollHeight;   // докрутить до конца, как рукой
  const r = el.getBoundingClientRect();
  lenta.scrollTop = 0;
  return {top: r.top, bottom: r.bottom};
}"""


def proverit_sostoyanie_v_kadre(str_, imya_sceny):
    """Карточка состояния держит ответ «я под защитой?» и не должна уезжать
    из кадра при прокрутке — она sticky (см. .sostoyanie в index.html).

    Диагноз 20.08: на сцене «сломалось» с открытым журналом лента вырастает
    выше окна, и заголовок статуса уезжал за верхний край — человек в момент
    беды не видел, что с ним. Докручиваем ленту до конца, как это делает
    человек мышью или колесом, и смотрим на getBoundingClientRect против
    высоты окна — а не полагаемся на память о том, что тут когда-то чинили.
    """
    r = str_.evaluate(SOSTOYANIE_V_KADRE_JS)
    if r is None:
        return []
    bedy = []
    if r["top"] < 0:
        bedy.append(f"{imya_sceny}: карточка состояния уезжает за верхний край "
                    f"на {round(-r['top'])}px при прокрутке ленты до конца")
    if r["bottom"] > VYSOTA:
        bedy.append(f"{imya_sceny}: карточка состояния уезжает за нижний край "
                    f"на {round(r['bottom'] - VYSOTA)}px")
    return bedy


def proverit_zhargon(str_, imya_sceny):
    """Ни одного машинного слова в том, что человек видит, ничего не открывая."""
    tekst = (str_.evaluate(VIDIMYY_TEKST_JS) or "").lower()
    bedy = []
    for slovo in ZHARGON:
        if slovo in tekst:
            mesto = tekst.index(slovo)
            kusok = tekst[max(0, mesto - 30):mesto + 40].replace("\n", " ⏎ ")
            bedy.append(f"{imya_sceny}: человеку видно машинное слово «{slovo}» — «…{kusok}…»")
    return bedy


def proverit_pokrytie():
    """Каждая заметка из konfig.go обязана попасть хотя бы на один снимок."""
    na_snimkah = {s.get("zametka", "") for s in SCENY.values()}
    bedy = []
    for imya, tekst in ZAMETKI.items():
        obrazec = tekst.split("%s")[0].strip()
        if not any(obrazec and obrazec in z for z in na_snimkah):
            bedy.append(f"покрытие: {imya} («{tekst[:44]}…») не показана ни в одной сцене — "
                        "человек увидит её раньше меня")
    return bedy


def kontrol_pereliva(br, port):
    """Щуп перелива обязан покраснеть, если строке запретить перенос."""
    sostoyanie["tek"] = SCENY["16_dlinnoe_imya"]
    str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
    str_.goto(f"http://127.0.0.1:{port}/index.html")
    str_.wait_for_timeout(700)
    str_.add_style_tag(content=".podpiska span { white-space: nowrap; }")
    str_.wait_for_timeout(200)
    bedy = proverit_pereliv(str_, "контроль-перелив")
    str_.close()
    return bedy


def kontrol_sostoyaniya(br, port):
    """Sticky-щуп обязан покраснеть, если карточку состояния расклеить.

    Проверяем ровно на той сцене, где беда была живой 20.08: «сломалось» с
    развёрнутым журналом — там лента вырастает выше окна. Снимаем sticky
    вручную (position:static) и смотрим, что проверка это заметит сама, без
    моей памятливости.
    """
    sostoyanie["tek"] = SCENY["5_slomalos"]
    str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
    str_.goto(f"http://127.0.0.1:{port}/index.html")
    str_.wait_for_timeout(700)
    str_.click("#knopka-zhurnal")
    str_.wait_for_timeout(400)
    str_.add_style_tag(content="#sostoyanie { position: static !important; }")
    str_.wait_for_timeout(200)
    bedy = proverit_sostoyanie_v_kadre(str_, "контроль-sticky")
    str_.close()
    return bedy


def kontrol_zhargona(br, port):
    """Гейт обязан покраснеть на заведомо жаргонной строке. Молчит — он мёртв."""
    sostoyanie["tek"] = dict(SCENY["4_rabotaet"],
                             zametka="прокси-режим: в профиле нет туннеля")
    str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
    str_.goto(f"http://127.0.0.1:{port}/index.html")
    str_.wait_for_timeout(700)
    bedy = proverit_zhargon(str_, "контроль-жаргон")
    str_.close()
    return bedy


def proverit_perevod(br, port):
    """Каждая примета обязана дать свой заголовок, а не правдоподобный чужой."""
    str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
    str_.goto(f"http://127.0.0.1:{port}/index.html")
    str_.wait_for_timeout(500)
    bedy = []
    for syroe, zhdem in PEREVOD_ZHDEM:
        dal = str_.evaluate("(s) => perevestiBedu(s).chto", syroe)
        if dal != zhdem:
            bedy.append(f'перевод: «{syroe[:52]}…» → «{dal}», а ждали «{zhdem}»')
    str_.close()
    return bedy


def snyat():
    from playwright.sync_api import sync_playwright
    VYHOD.mkdir(parents=True, exist_ok=True)
    vse_bedy = []
    with socketserver.TCPServer(("127.0.0.1", 0), Ruchki) as srv:
        port = srv.server_address[1]
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        with sync_playwright() as p:
            br = p.chromium.launch()
            for imya, sost in SCENY.items():
                sostoyanie["tek"] = sost
                str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
                str_.goto(f"http://127.0.0.1:{port}/index.html")
                str_.wait_for_timeout(700)
                if imya == "5_slomalos":  # журнал раскрыт: его видно только так
                    str_.click("#knopka-zhurnal")
                    str_.wait_for_timeout(400)
                # Снимок ПЕРВЫМ: щуп крутит страницу, а глазам нужен первый
                # кадр — то, что человек видит, ничего не тронув.
                put = VYHOD / f"{imya}.png"
                str_.screenshot(path=str(put))
                bedy = proverit_geometriyu(str_, imya)
                bedy += proverit_zhargon(str_, imya)
                bedy += proverit_pereliv(str_, imya)
                bedy += proverit_sostoyanie_v_kadre(str_, imya)
                if imya in ZHDEM_V_OKNE:
                    bedy += proverit_okno_bedy(str_, imya, ZHDEM_V_OKNE[imya])
                vse_bedy.extend(bedy)
                znak = "🔴" if bedy else "🟢"
                print(f"  {znak} {put}")
                for b in bedy:
                    print(f"      {b}")
                str_.close()
            perevod = proverit_perevod(br, port)
            if perevod:
                print()
                for b in perevod:
                    print(f"  🔴 {b}")
                vse_bedy.extend(perevod)
            else:
                print(f"\n  🈯 перевод беды: {len(PEREVOD_ZHDEM)} примет из {len(PEREVOD_ZHDEM)} легли верно")
            pokrytie = proverit_pokrytie()
            for b in pokrytie:
                print(f"  🔴 {b}")
            vse_bedy.extend(pokrytie)
            if not pokrytie:
                print(f"  🈯 заметки окна: {len(ZAMETKI)} из {len(ZAMETKI)} взяты "
                      "из konfig.go и показаны на снимках")
            kontrol = kontrol_shchupa(br, port)
            kontrol_zh = kontrol_zhargona(br, port)
            kontrol_per = kontrol_pereliva(br, port)
            kontrol_sost = kontrol_sostoyaniya(br, port)
            br.close()
        srv.shutdown()
    return vse_bedy, kontrol, kontrol_zh, kontrol_per, kontrol_sost


if __name__ == "__main__":
    print(f"облик: {OBLIK}")
    bedy, kontrol, kontrol_zh, kontrol_per, kontrol_sost = snyat()
    if kontrol_sost:
        print(f"\n  🧪 контроль sticky: щуп видит порчу — {kontrol_sost[0]}")
    else:
        print("\n🔴 ЩУП STICKY МЁРТВ: расклеенная карточка состояния его не разбудила.")
        sys.exit(2)
    if kontrol_per:
        print(f"\n  🧪 контроль перелива: щуп видит порчу — {kontrol_per[0]}")
    else:
        print("\n🔴 ЩУП ПЕРЕЛИВА МЁРТВ: строка, которой запретили перенос, его не разбудила.")
        sys.exit(2)
    if kontrol_zh:
        print(f"\n  🧪 контроль жаргона: гейт видит порчу — {kontrol_zh[0]}")
    else:
        print("\n🔴 ГЕЙТ ЖАРГОНА МЁРТВ: жаргонная строка в окне его не разбудила.")
        sys.exit(2)
    if kontrol:
        print(f"\n  🧪 контроль: щуп видит порчу ({len(kontrol)} находок), например:")
        print(f"      {kontrol[0]}")
    else:
        print("\n🔴 ЩУП МЁРТВ: на заведомо испорченной странице он смолчал. "
              "Зелень остальных сцен ничего не доказывает.")
        sys.exit(2)
    if bedy:
        print(f"\nКРАСНО: {len(bedy)} кнопок не достать в окне {SHIRINA}x{VYSOTA}.")
        sys.exit(1)
    print("\nВсе сцены зелёные.")
