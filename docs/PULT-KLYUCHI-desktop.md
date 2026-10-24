# Ключи пульта Kelevra Desktop

Создано из реестра клиента (`internal/sluzhba/pult.go`, `reestrDesktop`); править реестр, затем
`KELEVRA_OBNOVIT_KLYUCHI=1 go test ./internal/sluzhba -run TestTablicaKlyuchey`. Договор — KelevraServer
`docs/PULT.md`. Ключа нет в пульте — действует значение из кода (столбец «По умолчанию»), поведение прежнее.
Незнакомый ключ, не тот тип или выход за границы — ключ не принимается и уходит в `X-Pult` словом `oshibki`.

`umeet=1`. Команды: `diagnostika`, `obnovi_profil`, `pereydi_na_klyuch`, `prishli_zhurnal`, `proba`, `proverit_obnovlenie`, `sbros_avtomata`.

| Ключ | Тип | Границы | По умолчанию (= код) | Что подстраивает |
|---|---|---|---|---|
| `opros.info_min` | int | 5–240 | нет (сводка едет с профилем) | лёгкий опрос /info из службы с If-None-Match (SleditZaSvodkoy) |
| `zhurnal.otpravka_min` | int | 10–1440 | 60 | период отправки журнала (PeriodOtpravkiZhurnalov) |
| `zhurnal.chasto_do` | ISO-время | — | нет | до этого времени журнал каждые 10 мин |
| `obnovleniya.tiho` | bool | — | true | тихая установка найденного обновления (postavitTiho) |
| `obnovleniya.kanal` | выбор | obychnyy \| proba \| stop | obychnyy | proba — только сборка из пульта, stop — не обновляться; GitHub не спрашивается |
| `funkcii.avtorezhim` | bool | — | true | автомат дома/не дома (выключатель, сразу) |
| `kanal.dolgiy_opros` | bool | — | false | долгий опрос …/pult/zhdat (SleditZaDolgimOprosom) |
| `profil.period_min` | int | 15–1440 | obnovlyat_min из настроек (60), не меньше 15 | плановое обновление профиля и сводки (ObnovlyatProfil) |
| `obnovleniya.period_proverki_min` | int | 30–1440 | 240 | фоновая проверка обновления (SleditZaObnovleniem) |
| `obnovleniya.srok_proverki_s` | int | 5–300 | 30 | срок ответа на фоновую проверку (srokFonovoyProverkiObnovleniya) |
| `obnovleniya.srok_knopki_s` | int | 2–60 | 25 | срок ответа на «Проверить обновление» (srokProverkiObnovleniya) |
| `obnovleniya.srok_ustanovki_s` | int | 30–1800 | 180 | срок скачивания сборки (srokUstanovkiObnovleniya) |
| `zhurnal.povtor_posle_otkaza_min` | int | 5–1440 | 60 | повтор журнала после оборванной посылки (PovtorPosleOtkaza) |
| `zhurnal.povtor_do_peredachi_min` | int | 1–60 | 3 | повтор журнала после отказа до первого байта |
| `pravila.sverka_min` | int | 5–1440 | 30 | сверка наборов правил (SveryatPravila) |
| `set.ozhidanie_posle_sna_s` | int | 5–600 | 60 | ожидание сети после сна (srokOzhidaniyaSeti) |
| `avtomat.podtverzhdeniy` | int | 1–10 | 3 | подтверждений подряд для смены обстановки (Podtverzhdeniy) |
| `avtomat.podryad_do_prichiny` | int | 1–20 | 3 | слепых заходов до признания в окне (PodryadDoPrichiny) |
| `avtomat.pamyat_priznaka_s` | int | 5–600 | 45 | память DNS-признака дома (PamyatPriznaka) |
| `avtomat.interval_tikera_s` | int | 10–3600 | 120 | страховочный тикер служителя |
| `avtomat.pauza_drebezga_ms` | int | 100–30000 | 1000 | пауза после события смены сети (PauzaDrebezga) |
| `avtomat.pauza_bystrogo_s` | int | 1–60 | 5 | шаг быстрых опросов после события |
| `avtomat.povtorov_bystrogo` | int | 1–30 | 6 | число быстрых опросов после события |
| `avtomat.dns_domeny` | список строк | 1–10 шт. | youtube.com, discord.com, rutracker.org | контрольные домены DNS-зонда |
| `avtomat.dns_nuzhno` | int | 1–10 | 2 | сколько доменов должно быть подменено для признака дома |
| `avtomat.dns_kontrolnyy` | строка | — | gosuslugi.ru | контрольный домен (дома всегда настоящий) |
| `avtomat.dns_taimaut_ms` | int | 500–15000 | 3000 | бюджет DNS-зонда |
| `avtomat.domashnie_shlyuzy` | список строк | 0–20 шт. | зашитые (1 шт.) | MAC домашних шлюзов (опознание дома) |
| `avtomat.knopka_taimaut_s` | int | 2–60 | 8 | заход обстановки по кнопке «Подключиться» |
| `uzel.porog_otkazov` | int | 1–20 | 2 | провалов подряд до перехода на следующий узел |
| `uzel.vozvrat_na_pervyy_min` | int | 1–120 | 4 | сколько первый узел жив до возврата на него |
| `vykl.zapasnoy_put` | bool | — | false | выключатель: повтор запросов службы через туннель (zapasnoy_put.go) |
| `vykl.sverka_pravil` | bool | — | false | выключатель: сверка наборов правил с сервером |
| `vykl.pravila_iz_kesha` | bool | — | false | выключатель: правила с диска в конфиг ядра (ядро качает само, как до кеша) |
| `vykl.ochered_uzlov` | bool | — | false | выключатель: очередь узлов своей сети |
| `vykl.uchet_trafika` | bool | — | false | выключатель: учёт расхода этой машины (X-Device-Traffic) |
| `vykl.srochnyy_zhurnal` | bool | — | false | выключатель: срочная посылка журнала после неудачи связи |
| `vykl.zapisi_yadra` | bool | — | false | выключатель: записи с полями из журнала ядра |
