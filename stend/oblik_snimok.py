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

# Пока ядро не работает, настоящий обработчик (internal/sluzhba/sluzhba.go:
# uzly()) отдаёт список статикой из config.json — те же узлы, что и у живого
# ядра, но без zaderzhka: замер идёт только запросом ЧЕРЕЗ ядро, а его нет.
# Ключ "zaderzhka" тут просто отсутствует (как и делает GruppyStatik с omitempty),
# а не стоит нулём — 0 в окне читался бы как «быстрее всех», это была бы ложь.
UZLY_OTKLYUCHENO = {"gruppy": [{
    "imya": "Выбор узла", "sam": False, "seychas": "Нидерланды 2",
    "uzly": [
        {"imya": "Нидерланды 1"},
        {"imya": "Нидерланды 2"},
        {"imya": "Германия 1"},
        {"imya": "Финляндия 1"},
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
    "3_podnimaem": dict(BAZA, sost="podnimaem"),
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
    # Авторежим (internal/avtorezhim) опускает и поднимает защиту сам, а круг
    # с подзаголовком об этом ничего не знают — «отключено» / «трафик идёт
    # напрямую» выглядит ровно как поломка. Три сцены — по три исхода
    # avtorezhim_obstanovka (internal/avtorezhim/avtorezhim.go:51). Заметка
    # в «18» нарочно унаследовала СТАРУЮ s.zametka (как оно и бывает: s.kartina
    # не чистится при отключении, sluzhba.go) — сцена доказывает, что окно её
    # подменяет, а не показывает вперемешку с тем, что сейчас неправда.
    "18_avtorezhim_doma": dict(BAZA, sost="stoit", avtorezhim_vklyuchen=True,
                               avtorezhim_obstanovka="дома",
                               zametka=ZAMETKI["ZametkaBezTunnelya"]),
    "19_avtorezhim_vne_doma": dict(BAZA, sost="rabotaet", pid="8124", rezhim="tunnel",
                                   vniz_bayt=204_800_000, vverh_bayt=9_961_472,
                                   avtorezhim_vklyuchen=True, avtorezhim_obstanovka="вне дома",
                                   zametka=ZAMETKI["ZametkaVes"]),
    "20_avtorezhim_neizvestno": dict(BAZA, sost="stoit", avtorezhim_vklyuchen=True,
                                     avtorezhim_obstanovka="неизвестно"),
    # 22.08: /api/sostoyanie отдавало k.Zametka из последней сборки конфига
    # независимо от того, поднята ли защита сейчас (sluzhba.go: s.kartina не
    # чистится в OpustitZashchitu). Человек выключил защиту вручную — не
    # авторежимом, avtorezhim_vklyuchen=False — и всё равно читал «Защищены
    # только браузеры», хотя не защищено было НИЧЕГО. rezhim="proksi" тут
    # нарочно оставлен (сам по себе не враньё: он не показывается, пока
    # sost != rabotaet, — врёт именно zametka), zametka — пустая: ровно то,
    # что теперь честно отдаёт исправленная ручка.
    "21_vyklyucheno_vruchnuyu": dict(BAZA, sost="stoit", rezhim="proksi",
                                     mozhno_tun=True, zametka=""),
}

# Автозапуск, смена кода и журнал переехали на вкладку «Настройки» (спека
# 04.08: на «Сети» — только круг, заметка режима, кнопка полной защиты и
# список узлов). Без переключения вкладки эти сцены снимали бы вкладку
# «Сеть», где автозапуска попросту нет — то же немое исчезновение, что уже
# было 20.08 с этим самым переключателем.
NASTROYKI_SCENY = {"8_avtozapusk_vykl", "9_avtozapusk_vkl", "10_avtozapusk_ustarela"}

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
                              else UZLY_OTKLYUCHENO)
        if self.path.startswith("/api/obnovlenie"):
            # Сеть на стенде намеренно недоступна (это же честное поведение
            # живой ручки без интернета, sluzhba.go: obnovlenieRuchka) —
            # проверяем, что окно доводит это до понятной подписи, а не
            # виснет на «Проверяем…» (Вова 22.08, эталон — телефон).
            return self._json({"tekushchaya": BAZA["versiya"], "beda": "не удалось проверить"})
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
#      (зафиксированная снизу панель вкладок «#vkladki», модалка, наехавшая
#      карточка) — кнопка не тыкается, сколько её ни крути. Именно так ловится
#      перекос «padding-bottom у ленты — константа 84px, а панель вкладок
#      выросла больше неё»: панель выше константы — и последний ряд списка
#      навсегда под ней.
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


# Порча для контроля: делаем нижнюю панель вкладок заведомо выше, чем зазор,
# который лента держит под неё (--nizhnyaya, константа). Это ровно та беда,
# ради которой щуп и живёт: панель переросла зазор — и последний ряд списка
# навсегда под ней, никакой прокруткой не достать. До 21.08 нижней панелью
# была кнопка «.niz»; теперь на этом месте панель вкладок «#vkladki» (перенос
# вкладок вниз, эталон — телефон), а инвариант тот же самый.
PORCHA_CSS = "#vkladki { height: 140px !important; }"


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


# Заметка авторежима (id="zametka" — тот же блок, что несёт ZAMETKI выше)
# обязана назвать ПРИЧИНУ, а не молчать так же, как при поломке или ручном
# выключении. «19» держит две подстроки разом: доказывает, что новый текст
# не стирает старую заметку режима защиты («не ломай существующий текст»).
ZHDEM_AVTOREZHIM = {
    "18_avtorezhim_doma": ["обход блокировок уже делает роутер", "защита не нужна"],
    "19_avtorezhim_vne_doma": ["Защищён весь компьютер", "Защиту включил авторежим"],
    "20_avtorezhim_neizvestno": ["ещё смотрит, что это за сеть"],
}


# 22.08: заметка про ОБЪЁМ защиты («Защищены только браузеры», «Защищён весь
# компьютер» и соседи, ZAMETKI выше — все из konfig.go) переживала выключение
# защиты молча: s.kartina в sluzhba.go не чистится ни ручным тумблером
# (OpustitZashchitu), ни авторежимом. Правда только пока защита реально
# поднята (sost == rabotaet) — иначе человек читает про объём того, чего
# сейчас нет вовсе.
def proverit_net_zametki_obema(str_, imya_sceny):
    """Когда защита не поднята, окно не должно утверждать её объём.

    Сверяем с ZAMETKI из konfig.go, а не с вручную вписанной строкой: новая
    формулировка заметки поймается тем же щупом, без моей памятливости.
    """
    tekst = str_.evaluate("() => document.getElementById('zametka').textContent") or ""
    bedy = []
    for imya_zametki, obrazec in ZAMETKI.items():
        kusok = obrazec.split("%s")[0].strip()
        if kusok and kusok in tekst:
            bedy.append(f"{imya_sceny}: защита не поднята, а заметка всё ещё "
                        f"«{imya_zametki}»: «{tekst}»")
    return bedy


def proverit_avtorezhim(str_, imya_sceny, kuski):
    tekst = str_.evaluate("() => document.getElementById('zametka').textContent") or ""
    return [f"{imya_sceny}: заметка авторежима не сказала «{kusok}» (в блоке: «{tekst}»)"
            for kusok in kuski if kusok not in tekst]


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


# До этой правки тут была проверка «вкладки не уезжают из кадра при
# прокрутке» (SOSTOYANIE_V_KADRE_JS/proverit_sostoyanie_v_kadre/
# kontrol_sostoyaniya) — вкладки были sticky-переключателем ВНУТРИ
# скроллящейся .lenta, и щуп ловил ровно тот класс беды: расклей sticky
# (position:static) — переключатель уезжает вместе с содержимым.
# Правка 23.08 перенесла вкладки вниз панелью, как на телефоне (KTabBar,
# Components.kt): «#vkladki» теперь фиксирован на body, вне .lenta вообще —
# сам класс беды (sticky-элемент внутри скролл-контейнера теряет позицию)
# для него структурно не существует. Порча "position:static" на элементе,
# который в разметке не является потомком .lenta, не воспроизводит прежнюю
# беду и не должна была продолжать зеленеть молча — щуп честно покраснел на
# контроле («ЩУП «STICKY» МЁРТВ»), проверка снята. Реальную «панель выросла
# больше зазора и накрыла список» ловит kontrol_shchupa (PORCHA_CSS теперь
# растягивает именно #vkladki, см. выше).
OBREZKA_JS = """() => {
  const bedy = [];
  for (const el of document.querySelectorAll("*")) {
    if (!el.offsetParent) continue;                       // не виден — не мерим
    const svoy = [...el.childNodes].some(n => n.nodeType === 3 && n.textContent.trim());
    if (!svoy) continue;                                  // только листья со СВОИМ текстом
    const st = getComputedStyle(el);
    const dx = el.scrollWidth  - el.clientWidth;
    const dy = el.scrollHeight - el.clientHeight;
    const tekst = el.textContent.trim().replace(/\\s+/g, " ").slice(0, 40);
    // Режет только тот, кому нечем прокрутить: overflow:hidden. У ленты
    // (auto/scroll) переполнение законно — человек докрутит колесом.
    if (st.overflowX === "hidden" && dx > 2)
      bedy.push(`«${tekst}» обрезан по ширине на ${dx}px`);
    else if (st.overflowY === "hidden" && dy > 2)
      bedy.push(`«${tekst}» обрезан по высоте на ${dy}px`);
  }
  return bedy;
}"""


def proverit_obrezku(str_, imya_sceny):
    """Текст, срезанный многоточием, — это порча, а не «компактно».

    21.08: стенд светил 17 зелёных сцен, а в круге стояло «трафик браузеров ·…»
    — пара «режим · узел» не влезала в 144px и резалась line-clamp'ом. Ни один
    из четырёх щупов её не видел: геометрия мерила достижимость кнопок, перелив
    — уезд ЗА край окна. Обрезка не уезжает никуда, она молча съедает слово
    внутри своей рамки, и снаружи выглядит как штатная вёрстка.

    Мерим scrollWidth/scrollHeight против client* только там, где overflow
    скрыт: если у элемента есть прокрутка, переполнение — не потеря.
    """
    return [f"{imya_sceny}: {b}" for b in str_.evaluate(OBREZKA_JS)]


SLOVO_V_KRUGE_JS = """() => {
  const serdtse = document.querySelector(".krug-serdtse");
  // Границу берём у САМОЙ окружности, а не у её обёртки: обёртка 144px, а
  // круг внутри неё 130 (r=65) — семь пикселей запаса с каждой стороны,
  // ровно в которых 21.08 и пряталось торчащее слово.
  const okruzhnost = document.querySelector(".krug-fon");
  if (!serdtse || !serdtse.offsetParent || !okruzhnost) return [];
  const k = okruzhnost.getBoundingClientRect();
  const bedy = [];
  for (const el of serdtse.children) {
    if (!el.offsetParent || !el.textContent.trim()) continue;
    // Мерим САМ ТЕКСТ через Range, а не рамку блока: блок сидит в padding'е
    // круга и всегда внутри него, а за окружность вылезают именно буквы —
    // рамка о них не знает и покажет зелень (поймано 21.08 на «не
    // подключилось»: по блоку 2px, по буквам 20px).
    const rng = document.createRange(); rng.selectNodeContents(el);
    const r = rng.getBoundingClientRect();
    const tekst = el.textContent.trim().replace(/\\s+/g, " ").slice(0, 40);
    // Окружность вписана в обёртку, поэтому её горизонтальные края — края
    // обёртки. Текст, вылезший за них, ВИДНО торчащим по бокам круга.
    if (r.left < k.left - 1 || r.right > k.right + 1)
      bedy.push(`«${tekst}» торчит за круг на ` +
                `${Math.round(Math.max(k.left - r.left, r.right - k.right))}px`);
  }
  return bedy;
}"""


def proverit_slovo_v_kruge(str_, imya_sceny):
    """Круг — главная вещь окна, и слово обязано в него влезать.

    21.08: на четырёх сценах беды «не подключилось» торчало за окружность
    буквами с обеих сторон. Ни щуп обрезки (overflow тут visible — текст не
    режется, а вылезает), ни щуп перелива (за край ОКНА не уехало) этого не
    видят: порча живёт между их зонами.
    """
    return [f"{imya_sceny}: {b}" for b in str_.evaluate(SLOVO_V_KRUGE_JS)]


def kontrol_slova_v_kruge(br, port):
    """Щуп обязан покраснеть на той самой строке, из-за которой появился.

    21.08 круг был 144px, и «не подключилось» торчало само, без порчи. Круг
    вырос до 210dp (эталон телефона: Kelevra.kt, KDim.DialSize) — то же слово
    теперь сворачивается на две строки и укладывается внутри без порчи. Порчу
    делаем явной тем же приёмом, что и kontrol_pereliva: запрещаем перенос
    (white-space:nowrap) — ровно от того, чем .krug-slovo защищается по
    умолчанию (перенос + класс .dlinnoe уменьшает кегль).
    """
    sostoyanie["tek"] = SCENY["5_slomalos"]
    str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
    str_.goto(f"http://127.0.0.1:{port}/index.html")
    str_.wait_for_timeout(700)
    str_.evaluate("() => { document.getElementById('zvanie').textContent = 'не подключилось совсем'; }")
    str_.add_style_tag(content=".krug-slovo { white-space: nowrap; }")
    str_.wait_for_timeout(200)
    bedy = proverit_slovo_v_kruge(str_, "контроль-круг")
    str_.close()
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


# --- 23.08: экран настроек был «по-свойски» (Вова, 22.08: «настройки тоже
# написаны по свойски и не то, что я хотел бы видеть», «почему надписи такие
# будто ты по братски кому то пишешь») — три голые кнопки без единого
# пояснения, чужие заголовки блоков, значок версии не на месте (сравнили с
# эталоном продукта — экраном настроек телефона, SimpleSettingsScreen.kt).
# Щупы ниже держат форму эталона так, чтобы регресс к «по-свойски» покраснел
# сам, без моей памятливости.

ZAGOLOVKI_BLOKOV_NASTROEK = {"Сеть", "Приложение", "Подписка"}


def proverit_podpisi_nastroyek(str_, imya_sceny):
    """Каждый пункт настроек обязан объяснять себя второй строкой-подписью.

    До правки было три голые кнопки без единого пояснения — человек не
    понимал, что делает пункт (та самая жалоба 22.08).
    """
    bez_podpisi = str_.evaluate("""() => {
      const bez = [];
      // .tihaya-tekst — только те пункты, что ПЕРЕСТРОЕНЫ в заголовок+подпись
      // (обновление/журнал/код доступа). «Скопировать журнал» — не пункт
      // настроек, а разовое подтверждение действия, эталон его не требует.
      document.querySelectorAll('#tab-nastroyki .tihaya-tekst').forEach((t) => {
        const p = t.querySelector('.tihaya-podpis');
        const knopka = t.closest('button');
        if (!p || !p.textContent.trim()) bez.push((knopka && knopka.id) || 'без id');
      });
      document.querySelectorAll('#tab-nastroyki .perekl-ryad').forEach((r) => {
        const p = r.querySelector('.perekl-tekst .podpis');
        if (!p || !p.textContent.trim()) bez.push(r.id || 'ряд без id');
      });
      return bez;
    }""")
    return [f"{imya_sceny}: пункт настроек «{b}» без подписи-объяснения" for b in bez_podpisi]


def proverit_zagolovki_blokov(str_, imya_sceny):
    """Заголовки блоков — ровно «Сеть»/«Приложение»/«Подписка», эталон телефона.

    Было «Приложение»/«Доступ»/«Если что-то не так» — последний и есть та
    самая «по-свойски» надпись (Вова 22.08).
    """
    zagi = str_.evaluate("""() => [...document.querySelectorAll('#tab-nastroyki .zagolovok-bloka')]
        .map((z) => z.textContent.trim())""")
    if set(zagi) != ZAGOLOVKI_BLOKOV_NASTROEK:
        return [f"{imya_sceny}: заголовки блоков настроек {zagi}, а ждали ровно "
                f"{sorted(ZAGOLOVKI_BLOKOV_NASTROEK)}"]
    return []


def proverit_podval_versii(str_, imya_sceny, versiya):
    """Подвал «KELEVRA <версия>» — эталон телефона (BuildConfig.VERSION_NAME)."""
    tekst = str_.evaluate("() => document.getElementById('podval-versiya').textContent") or ""
    if f"KELEVRA {versiya}" not in tekst:
        return [f"{imya_sceny}: подвал настроек «{tekst}», а ждали версию «KELEVRA {versiya}»"]
    return []


def proverit_net_versii_v_shapke(str_, imya_sceny):
    """Значка версии в шапке быть не должно — на телефоне его там нет вовсе."""
    tekst = str_.evaluate("() => document.getElementById('chip-tekst').textContent") or ""
    if re.match(r"^v\d", tekst.strip()):
        return [f"{imya_sceny}: в шапке всё ещё значок версии «{tekst}» — эталон (телефон) его не показывает"]
    return []


def proverit_obnovlenie(str_, imya_sceny):
    """«Проверить обновление» обязан довести подпись до понятного слова.

    Сеть на стенде намеренно недоступна — эндпойнт (sluzhba.go:
    obnovlenieRuchka) обязан ответить понятной подписью, а не повесить
    строку на «Проверяем…» навсегда.
    """
    str_.click("#knopka-obnovlenie")
    str_.wait_for_timeout(400)
    tekst = (str_.evaluate("() => document.getElementById('podpis-obnovlenie').textContent") or "").strip()
    if not tekst or tekst == "Проверяем…":
        return [f"{imya_sceny}: «Проверить обновление» зависло на «{tekst}»"]
    return []


def kontrol_podpisi_nastroyek(br, port):
    """Щуп подписей обязан покраснеть, если подпись у пункта стереть."""
    sostoyanie["tek"] = SCENY["8_avtozapusk_vykl"]
    str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
    str_.goto(f"http://127.0.0.1:{port}/index.html")
    str_.wait_for_timeout(700)
    str_.click("#vkladka-nastroyki")
    str_.wait_for_timeout(200)
    str_.evaluate("""() => {
      document.querySelectorAll('#tab-nastroyki .tihaya-podpis, #tab-nastroyki .perekl-tekst .podpis')
        .forEach((p) => { p.textContent = ''; });
    }""")
    bedy = proverit_podpisi_nastroyek(str_, "контроль-подписи")
    str_.close()
    return bedy


def kontrol_zagolovkov_blokov(br, port):
    """Щуп заголовков обязан покраснеть на старой «по-свойски» надписи."""
    sostoyanie["tek"] = SCENY["8_avtozapusk_vykl"]
    str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
    str_.goto(f"http://127.0.0.1:{port}/index.html")
    str_.wait_for_timeout(700)
    str_.click("#vkladka-nastroyki")
    str_.wait_for_timeout(200)
    str_.evaluate("""() => {
      const z = document.querySelector('#tab-nastroyki .zagolovok-bloka');
      z.textContent = 'Если что-то не так';
    }""")
    bedy = proverit_zagolovki_blokov(str_, "контроль-заголовки")
    str_.close()
    return bedy


def kontrol_podvala_versii(br, port):
    """Щуп подвала обязан покраснеть, если версия из него пропадёт."""
    sostoyanie["tek"] = SCENY["8_avtozapusk_vykl"]
    str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
    str_.goto(f"http://127.0.0.1:{port}/index.html")
    str_.wait_for_timeout(700)
    str_.click("#vkladka-nastroyki")
    str_.wait_for_timeout(200)
    str_.evaluate("() => { document.getElementById('podval-versiya').textContent = ''; }")
    bedy = proverit_podval_versii(str_, "контроль-подвал", BAZA["versiya"])
    str_.close()
    return bedy


def kontrol_versii_v_shapke(br, port):
    """Щуп шапки обязан покраснеть, если значок версии в неё вернуть."""
    sostoyanie["tek"] = SCENY["2_otklyucheno"]
    str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
    str_.goto(f"http://127.0.0.1:{port}/index.html")
    str_.wait_for_timeout(700)
    str_.evaluate("() => { document.getElementById('chip-tekst').textContent = 'v0.5.3'; }")
    bedy = proverit_net_versii_v_shapke(str_, "контроль-версия-в-шапке")
    str_.close()
    return bedy


def kontrol_obnovleniya(br, port):
    """Щуп обновления обязан покраснеть, если подпись зависнет на «Проверяем…»."""
    sostoyanie["tek"] = SCENY["8_avtozapusk_vykl"]
    str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
    str_.goto(f"http://127.0.0.1:{port}/index.html")
    str_.wait_for_timeout(700)
    str_.click("#vkladka-nastroyki")
    str_.wait_for_timeout(200)
    str_.evaluate("() => { window.podpisObnovleniya = () => 'Проверяем…'; }")
    bedy = proverit_obnovlenie(str_, "контроль-обновление")
    str_.close()
    return bedy


def kontrol_obrezki(br, port):
    """Щуп обрезки обязан покраснеть на строке, которой ужали рамку.

    Порчу берём ту самую, из-за которой щуп и появился: возвращаем в круг
    пару «режим · узел» длиной больше двух строк. Круг вырос до 210dp (эталон
    телефона), а .krug-pod вместе с ним — до 150px, и прежняя фраза «трафик
    браузеров · Нидерланды 2» стала укладываться в две строки без обрезки.
    Берём фразу заведомо длиннее. Если после такой подмены щуп молчит —
    молчание на чистых сценах ничего не стоит.
    """
    sostoyanie["tek"] = SCENY["4_rabotaet"]
    str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
    str_.goto(f"http://127.0.0.1:{port}/index.html")
    str_.wait_for_timeout(700)
    str_.evaluate("() => { document.getElementById('podzagolovok').textContent = "
                  "'трафик браузеров · Нидерланды 2 · дополнительный резервный узел'; }")
    str_.wait_for_timeout(200)
    bedy = proverit_obrezku(str_, "контроль-обрезка")
    str_.close()
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


def kontrol_zametki_obema(br, port):
    """Щуп обязан покраснеть, если Zametka переживёт выключение защиты.

    Порча — ровно баг 22.08: подсовываем сцену «выключено вручную» с тем
    самым текстом про объём защиты, который раньше уезжал бы на этот экран
    (до правки sluzhba.go отдавал бы k.Zametka безусловно). Молчит — щуп мёртв.
    """
    sostoyanie["tek"] = dict(SCENY["21_vyklyucheno_vruchnuyu"],
                             zametka=ZAMETKI["ZametkaBezTunnelya"])
    str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
    str_.goto(f"http://127.0.0.1:{port}/index.html")
    str_.wait_for_timeout(700)
    bedy = proverit_net_zametki_obema(str_, "контроль-заметка")
    str_.close()
    return bedy


def kontrol_avtorezhima(br, port):
    """Щуп заметки авторежима обязан покраснеть, если объяснение из неё убрать.

    Порча — ровно та, ради которой заметка появилась: авторежим опустил
    защиту («дома»), а окно молчит об этом так же, как при поломке или
    ручном выключении. Глушим функцию, которая пишет причину в заметку
    (zametkaAvtorezhima в index.html), и смотрим, что проверка это заметит
    сама, без моей памятливости.
    """
    sostoyanie["tek"] = SCENY["18_avtorezhim_doma"]
    str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
    str_.goto(f"http://127.0.0.1:{port}/index.html")
    str_.wait_for_timeout(700)
    str_.evaluate("async () => { window.zametkaAvtorezhima = () => null; await obnovit(); }")
    str_.wait_for_timeout(200)
    bedy = proverit_avtorezhim(str_, "контроль-авторежим", ZHDEM_AVTOREZHIM["18_avtorezhim_doma"])
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
                if imya in NASTROYKI_SCENY:
                    str_.click("#vkladka-nastroyki")
                    str_.wait_for_timeout(200)
                # Журнал переехал на вкладку «Настройки» (спека 04.08: круг
                # и ошибка живут на «Сети», журнал — «всё остальное» на
                # «Настройках») — сцена «сломалось» теперь снимает вкладку
                # «Сеть» как есть, открытый журнал ловят проверки геометрии/
                # обрезки ниже. Снимок ПЕРВЫМ: щуп крутит страницу, а глазам
                # нужен первый кадр — то, что человек видит, ничего не тронув.
                put = VYHOD / f"{imya}.png"
                str_.screenshot(path=str(put))
                bedy = proverit_geometriyu(str_, imya)
                bedy += proverit_zhargon(str_, imya)
                bedy += proverit_pereliv(str_, imya)
                bedy += proverit_obrezku(str_, imya)
                bedy += proverit_slovo_v_kruge(str_, imya)
                bedy += proverit_net_versii_v_shapke(str_, imya)
                if imya in ZHDEM_V_OKNE:
                    bedy += proverit_okno_bedy(str_, imya, ZHDEM_V_OKNE[imya])
                if imya in ZHDEM_AVTOREZHIM:
                    bedy += proverit_avtorezhim(str_, imya, ZHDEM_AVTOREZHIM[imya])
                if sost.get("sost") != "rabotaet":
                    bedy += proverit_net_zametki_obema(str_, imya)
                if imya in NASTROYKI_SCENY:
                    bedy += proverit_podpisi_nastroyek(str_, imya)
                    bedy += proverit_zagolovki_blokov(str_, imya)
                    bedy += proverit_podval_versii(str_, imya, sost["versiya"])
                    bedy += proverit_obnovlenie(str_, imya)
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
            kontroli = {
                "круга": (kontrol_slova_v_kruge(br, port),
                          "слово, торчащее за окружность, его не разбудило"),
                "обрезки": (kontrol_obrezki(br, port),
                            "строка, которой ужали рамку, его не разбудила"),
                "перелива": (kontrol_pereliva(br, port),
                             "строка, которой запретили перенос, его не разбудила"),
                "жаргона": (kontrol_zhargona(br, port),
                            "жаргонная строка в окне его не разбудила"),
                "достижимости": (kontrol_shchupa(br, port),
                                 "на заведомо испорченной странице он смолчал"),
                "авторежима": (kontrol_avtorezhima(br, port),
                               "заметка авторежима его не разбудила"),
                "заметки объёма": (kontrol_zametki_obema(br, port),
                                   "заметка объёма, пережившая выключение защиты, его не разбудила"),
                "подписей настроек": (kontrol_podpisi_nastroyek(br, port),
                                      "пункт настроек без подписи его не разбудил"),
                "заголовков блоков": (kontrol_zagolovkov_blokov(br, port),
                                      "старая «по-свойски» надпись блока его не разбудила"),
                "подвала версии": (kontrol_podvala_versii(br, port),
                                   "пропавшая версия в подвале его не разбудила"),
                "версии в шапке": (kontrol_versii_v_shapke(br, port),
                                   "вернувшийся значок версии в шапке его не разбудил"),
                "обновления": (kontrol_obnovleniya(br, port),
                               "подпись, зависшая на «Проверяем…», его не разбудила"),
            }
            br.close()
        srv.shutdown()
    return vse_bedy, kontroli


if __name__ == "__main__":
    print(f"облик: {OBLIK}")
    bedy, kontroli = snyat()
    for imya, (nashel, pochemu) in kontroli.items():
        if nashel:
            print(f"\n  🧪 контроль {imya}: щуп видит порчу — {nashel[0]}")
        else:
            print(f"\n🔴 ЩУП «{imya.upper()}» МЁРТВ: {pochemu}. "
                  "Зелень остальных сцен ничего не доказывает.")
            sys.exit(2)
    if bedy:
        print(f"\nКРАСНО: {len(bedy)} бед в окне {SHIRINA}x{VYSOTA}.")
        sys.exit(1)
    print("\nВсе сцены зелёные.")
