#!/usr/bin/env python3
"""Живой стенд ОБЕИХ дверей к /api/obnovlenie_postavit из окна:

  * диалог «Есть обновление» с кнопкой «ОБНОВИТЬ» — главный путь с 02.09.
    Он поднимается сам, как только фон нашёл версию новее, и с этого дня
    именно он, а не пузырь трея, разговаривает с человеком при открытом окне
    (internal/sluzhba: povestitEsliNovaya). Сцена «успех» ходит через него.
  * пункт «Установить версию X» в настройках (#knopka-obnovlenie, 28.08) —
    вторая дверь к ТОЙ ЖЕ ручке, для того, кто диалог закрыл кнопкой «ПОЗЖЕ».
    Сцена «отказ» ходит через него, закрыв диалог, — заодно доказывая, что
    закрытый диалог не лезет обратно поверх экрана.

До 02.09 обе сцены жали пункт настроек, и появление диалога сломало сам щуп:
playwright 58 раз подряд сообщал «<div id="dialog-obnovleniya-fon"> intercepts
pointer events» — модальный фон честно перехватывал клик, а щуп краснел не про
продукт, а про себя. Дверей стало две, и щуп теперь ходит в обе.

Обе двери зовут /api/obnovlenie_postavit — ровно ту ручку, что уже дёргает
тычок в пузырь трея. stend/oblik_snimok.py доказывает окно против ПОДСТАВНОЙ /api/*
(http.server, свои JSON) — этого достаточно для вёрстки, но не для того,
дошёл ли клик до настоящей ручки: подставной сервер отдаёт что скажешь, а не
что решила служба.

Этот стенд поднимает НАСТОЯЩУЮ службу (--sluzhba, как stend/obnovlenie_postavit.sh,
который уже доказывает сторону СЛУЖБЫ этой же ручки по голому HTTP) с
поддельным ИСТОЧНИКОМ обновлений (KELEVRA_RELIZY → локальный http.server
вместо GitHub) и водит по её настоящей странице playwright — тем же приёмом,
что и stend/oblik_snimok.py. Дошёл ли клик до ручки, судим НЕ перехватом
запроса в браузере, а фактом на диске службы (файл подменился на новую
сборку) — той же уликой, что уже использует obnovlenie_postavit.sh.

    python3 stend/oblik_ustanovka_obnovleniya.py
"""
import hashlib
import http.server
import json
import os
import shutil
import socketserver
import subprocess
import sys
import threading
import time
import urllib.request
from pathlib import Path

KOREN = Path(__file__).resolve().parent.parent
STEND = KOREN / ".stend" / "oblik_ustanovka_obnovleniya"

# Стенду нужен компилятор, а вызывающая оболочка не всегда та же, что у
# человека: `go` живёт вне PATH служб, а сборка без GOCACHE падает насмерть
# («build cache is required»). Соседние стенды берут $HOME/.cache/go-build
# (obnovlenie_postavit.sh:25) — но при пустом $HOME это неписуемый /.cache,
# поэтому запасной кеш кладём рядом с деревом, а не в корень.
if not shutil.which("go") and Path("/usr/local/go/bin/go").exists():
    os.environ["PATH"] = os.environ.get("PATH", "") + ":/usr/local/go/bin"
if not os.environ.get("GOCACHE"):
    domashniy = Path(os.environ.get("HOME", "")) / ".cache" / "go-build"
    zapasnoy = KOREN.parent / ".gocache"
    os.environ["GOCACHE"] = str(domashniy if os.environ.get("HOME") else zapasnoy)
RELIZY_DOM = STEND / "relizy"
STARAYA_VERSIYA = "0.6.40"
NOVAYA_VERSIYA = "0.6.41"
BIN_STARAYA = STEND / "kelevra_staraya"
BIN_NOVAYA = RELIZY_DOM / "Kelevra"
SHIRINA, VYSOTA = 420, 660

procesy = []  # subprocess.Popen — уборка в конце
doma = []  # каталоги KELEVRA_DIR запущенных копий — по ним же и pkill


def md5(put):
    try:
        return hashlib.md5(Path(put).read_bytes()).hexdigest()
    except FileNotFoundError:
        return None


def pochistit():
    for p in procesy:
        try:
            p.kill()
        except Exception:
            pass
    for p in procesy:
        try:
            p.wait(timeout=3)
        except Exception:
            pass
    # Тычок в пузырь после успеха поднимает СМЕНУ отдельным процессом (тот же
    # путь, что и в obnovlenie_postavit.sh) — она не наш прямой ребёнок,
    # добиваем по каталогу KELEVRA_DIR, в котором она сидит.
    for dom in doma:
        subprocess.run(["pkill", "-KILL", "-f", str(dom)], check=False)
    time.sleep(0.3)


def postroit_bin(put, versiya):
    ldflags = f"-X github.com/HRYNdev/kelevra-desktop/internal/podpiska.Versiya={versiya}"
    log = STEND / f"build_{versiya}.log"
    r = subprocess.run(
        ["go", "build", "-ldflags", ldflags, "-o", str(put), "./cmd/kelevra"],
        cwd=KOREN, stdout=open(log, "w"), stderr=subprocess.STDOUT,
    )
    if r.returncode != 0:
        print(f"🔴 сборка версии {versiya} не прошла:\n{log.read_text()}")
        sys.exit(2)
    put.chmod(0o755)


def pishi_relizy(port, razmer):
    RELIZY_DOM.joinpath("relizy.json").write_text(json.dumps([{
        "tag_name": f"app-v{NOVAYA_VERSIYA}", "draft": False, "prerelease": False,
        "assets": [{"name": "Kelevra.exe",
                    "browser_download_url": f"http://127.0.0.1:{port}/Kelevra",
                    "size": razmer}],
    }]))


def zapusti_relizy_server():
    class Otdacha(http.server.SimpleHTTPRequestHandler):
        def __init__(self, *a, **kw):
            super().__init__(*a, directory=str(RELIZY_DOM), **kw)

        def log_message(self, *a):
            pass

    srv = socketserver.TCPServer(("127.0.0.1", 0), Otdacha)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv, srv.server_address[1]


def zapusti_sluzhbu(imya, port_relizy, bez_fona=True):
    """Настоящий бинарь --sluzhba со СТАРОЙ версией и поддельным источником
    обновлений — тот же приём, что и stend/obnovlenie_postavit.sh:
    zapusti_sluzhbu(). bez_fona=True держит копию БЕЗ мгновенной фоновой
    проверки на старте (KELEVRA_BEZ_OBNOVLENIYA=1) — иначе пункт 1
    («Проверить обновление» до находки) недоказуем: находка приезжает за
    доли секунды сама.
    """
    dom = STEND / f"dom_{imya}"
    dom.mkdir(parents=True, exist_ok=True)
    shutil.copy(BIN_STARAYA, dom / "Kelevra")
    (dom / "Kelevra").chmod(0o755)
    # Кнопка «Установить обновление» живёт на вкладке «Настройки», а нижняя
    # панель вкладок скрыта, пока не введён код (index.html: $("vkladki").hidden
    # = nuzhenKod). Настоящий сервер подписки в стенде недоступен и не нужен —
    # проверка кнопки обновления не зависит от профиля, только от kod_est
    # (s.Nastroyki.Kod != ""), поэтому код кладём готовым файлом настроек, как
    # у машины, где его уже когда-то сохранили — тем же способом, каким
    # hranenie.Sohranit пишет его на диск.
    (dom / "nastroyki.json").write_text(json.dumps({
        "kod": "STEND-KOD", "prava_zaprosheny": True,
    }))
    log = STEND / f"sluzhba_{imya}.log"
    env = dict(os.environ)
    env["KELEVRA_DIR"] = str(dom)
    env["KELEVRA_RELIZY"] = f"http://127.0.0.1:{port_relizy}/relizy.json"
    if bez_fona:
        env["KELEVRA_BEZ_OBNOVLENIYA"] = "1"
    p = subprocess.Popen([str(dom / "Kelevra"), "--sluzhba"],
                          stdout=open(log, "w"), stderr=subprocess.STDOUT, env=env)
    procesy.append(p)
    doma.append(dom)
    adr = None
    for _ in range(40):
        if log.exists():
            for stroka in log.read_text(errors="replace").splitlines():
                if stroka.startswith("KELEVRA-SLUZHBA "):
                    adr = stroka.split(" ", 1)[1].strip()
                    break
        if adr or p.poll() is not None:
            break
        time.sleep(0.25)
    if not adr:
        print(f"⚫ ПРИБОР МЁРТВ: копия «{imya}» не подняла HTTP за 10с — окно НЕ проверялось")
        print(log.read_text(errors="replace"))
        pochistit()
        sys.exit(7)
    return adr, dom, p


def poluchit(url):
    with urllib.request.urlopen(url, timeout=5) as o:
        return json.loads(o.read())


def sostoyanie(adr):
    return poluchit(adr + "api/sostoyanie")


def proverit_seychas(adr):
    urllib.request.urlopen(urllib.request.Request(adr + "api/obnovlenie_proverit", method="POST"), timeout=5).read()


def dozhdatsya_nahodki(adr, imya):
    for _ in range(20):
        s = sostoyanie(adr)
        if s.get("novaya_versiya_dostupna") == NOVAYA_VERSIYA:
            return
        time.sleep(0.25)
    print(f"🔴 {imya}: фон не нашёл {NOVAYA_VERSIYA} за 5с — сцене нечего ставить, дальше проверять нечего")
    pochistit()
    sys.exit(1)


def tekst(str_, id_):
    return (str_.evaluate(f"() => document.getElementById('{id_}').textContent") or "").strip()


def scena_uspeh(br, port_relizy):
    bedy = []
    adr, dom, _ = zapusti_sluzhbu("uspeh", port_relizy, bez_fona=True)
    str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
    str_.goto(adr)
    str_.wait_for_timeout(700)
    str_.click("#vkladka-nastroyki")
    str_.wait_for_timeout(300)

    zag = tekst(str_, "zagolovok-obnovlenie")
    if zag != "Проверить обновление":
        bedy.append(f"пункт 1: заголовок кнопки без находки — «{zag}», ждал «Проверить обновление»")
    else:
        print("  🟢 пункт 1: заголовок «Проверить обновление» до находки")

    proverit_seychas(adr)
    dozhdatsya_nahodki(adr, "успех")
    str_.evaluate("async () => { await obnovit(); }")
    str_.wait_for_timeout(200)

    zag = tekst(str_, "zagolovok-obnovlenie")
    zhdem_zag = f"Установить версию {NOVAYA_VERSIYA}"
    if zag != zhdem_zag:
        bedy.append(f"пункт 2: заголовок после находки — «{zag}», ждал «{zhdem_zag}»")
    else:
        print(f"  🟢 пункт 2: заголовок «{zhdem_zag}»")

    # Диалог обязан подняться САМ, без единого клика: с 02.09 это и есть то,
    # чем окно сообщает про находку (а пузырь трея при открытом окне молчит).
    try:
        str_.wait_for_selector("#dialog-obnovleniya-fon", state="visible", timeout=5000)
        print("  🟢 пункт 2а: диалог «Есть обновление» поднялся сам")
    except Exception:
        bedy.append("пункт 2а: диалог «Есть обновление» не поднялся сам — при открытом окне "
                    "человеку про находку сказать больше нечем (пузырь трея теперь молчит)")

    dialog_versiya = tekst(str_, "dialog-obnovleniya-versiya")
    if dialog_versiya != f"ВЕРСИЯ {NOVAYA_VERSIYA}":
        bedy.append(f"пункт 2б: в диалоге «{dialog_versiya}», ждал «ВЕРСИЯ {NOVAYA_VERSIYA}»")
    else:
        print(f"  🟢 пункт 2б: в диалоге «ВЕРСИЯ {NOVAYA_VERSIYA}»")

    md5_do = md5(dom / "Kelevra")
    str_.click("#dialog-obnovlenie-postavit")  # главная дверь: кнопка «ОБНОВИТЬ» в диалоге
    try:
        str_.wait_for_function(
            "() => document.getElementById('podpis-obnovlenie').textContent !== 'Устанавливаем…'",
            timeout=8000)
    except Exception:
        pass
    str_.wait_for_timeout(200)

    md5_posle = md5(dom / "Kelevra")
    md5_reliz = md5(BIN_NOVAYA)
    if md5_posle == md5_do or md5_posle != md5_reliz:
        bedy.append(f"пункт 3: клик НЕ дошёл до /api/obnovlenie_postavit — файл на диске службы не стал "
                    f"новой сборкой (до={md5_do}, после={md5_posle}, релиз={md5_reliz})")
    else:
        print("  🟢 пункт 3: файл на диске службы подменился на новую сборку — клик дошёл до ручки")

    pod = tekst(str_, "podpis-obnovlenie")
    if pod != "Готово, Kelevra перезапускается…":
        bedy.append(f"пункт 4: подпись после успеха — «{pod}», ждал «Готово, Kelevra перезапускается…»")
    else:
        print("  🟢 пункт 4: подпись «Готово, Kelevra перезапускается…»")

    # Диалог и пункт настроек показывают ОДНО состояние и обязаны говорить одно
    # и то же: разойдись они — диалог сказал бы «Готово», а строка под ним
    # «нажмите, чтобы поставить».
    hod = tekst(str_, "dialog-obnovleniya-hod")
    if hod != "Готово, Kelevra перезапускается…":
        bedy.append(f"пункт 4а: в диалоге после успеха — «{hod}», ждал «Готово, Kelevra перезапускается…»")
    else:
        print("  🟢 пункт 4а: диалог говорит то же, что и пункт настроек")

    str_.close()
    return bedy


def scena_otkaz(br, port_relizy, razmer_relizy):
    bedy = []
    # Релиз объявляет размер на 1 байт больше настоящего — Postavit() сверяет
    # скачанное с объявленным размером и отказывает, файл не трогает (тот же
    # приём, что сцена «в» в stend/obnovlenie_postavit.sh).
    pishi_relizy(port_relizy, razmer_relizy + 1)
    adr, dom, _ = zapusti_sluzhbu("otkaz", port_relizy, bez_fona=True)
    str_ = br.new_page(viewport={"width": SHIRINA, "height": VYSOTA})
    str_.goto(adr)
    str_.wait_for_timeout(700)
    str_.click("#vkladka-nastroyki")
    str_.wait_for_timeout(300)

    proverit_seychas(adr)
    dozhdatsya_nahodki(adr, "отказ")
    str_.evaluate("async () => { await obnovit(); }")
    str_.wait_for_timeout(200)

    # ВТОРАЯ ДВЕРЬ. Диалог поднялся сам — закрываем его кнопкой «ПОЗЖЕ», как
    # это делает человек, который ставить прямо сейчас не хочет, и идём тем
    # путём, что был единственным до 02.09: пункт в настройках. Заодно
    # проверяется, что закрытый диалог не лезет обратно поверх экрана: не
    # закройся он по-настоящему, клик ниже снова перехватил бы модальный фон.
    str_.click("#dialog-obnovlenie-pozzhe")
    str_.wait_for_timeout(300)
    if str_.is_visible("#dialog-obnovleniya-fon"):
        bedy.append("пункт 5а: диалог не закрылся по «ПОЗЖЕ» — от него нельзя отделаться")
    else:
        print("  🟢 пункт 5а: диалог закрылся по «ПОЗЖЕ» и обратно не полез")

    str_.click("#knopka-obnovlenie")
    try:
        str_.wait_for_function(
            "() => document.getElementById('podpis-obnovlenie').textContent !== 'Устанавливаем…'",
            timeout=8000)
    except Exception:
        pass
    str_.wait_for_timeout(200)

    pod = tekst(str_, "podpis-obnovlenie")
    if pod != "Не удалось поставить, нажмите ещё раз":
        bedy.append(f"пункт 5: подпись после отказа ручки — «{pod}», ждал «Не удалось поставить, нажмите ещё раз»")
    else:
        print("  🟢 пункт 5: подпись «Не удалось поставить, нажмите ещё раз»")

    str_.close()
    return bedy


def snyat():
    from playwright.sync_api import sync_playwright
    shutil.rmtree(STEND, ignore_errors=True)
    STEND.mkdir(parents=True)
    RELIZY_DOM.mkdir(parents=True)

    print("── сборка старой и новой версий ──")
    postroit_bin(BIN_STARAYA, STARAYA_VERSIYA)
    postroit_bin(BIN_NOVAYA, NOVAYA_VERSIYA)
    razmer = BIN_NOVAYA.stat().st_size

    srv, port_relizy = zapusti_relizy_server()
    pishi_relizy(port_relizy, razmer)
    print(f"── поддельный источник обновлений на 127.0.0.1:{port_relizy}, {STARAYA_VERSIYA} → {NOVAYA_VERSIYA} ({razmer} байт) ──")

    vse_bedy = []
    with sync_playwright() as p:
        br = p.chromium.launch()
        print("сцена «успех»:")
        vse_bedy += scena_uspeh(br, port_relizy)
        print("сцена «отказ ручки»:")
        vse_bedy += scena_otkaz(br, port_relizy, razmer)
        br.close()
    srv.shutdown()
    return vse_bedy


if __name__ == "__main__":
    try:
        bedy = snyat()
    finally:
        pochistit()
    if bedy:
        print(f"\nКРАСНО: {len(bedy)} бед в кнопке «Установить»:")
        for b in bedy:
            print(f"  🔴 {b}")
        sys.exit(1)
    print("\nВсе 5 пунктов зелёные: настоящая служба, настоящее окно, клик дошёл до /api/obnovlenie_postavit.")
