package sluzhba

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
	"github.com/HRYNdev/kelevra-desktop/internal/konfig"
	"github.com/HRYNdev/kelevra-desktop/internal/zapasnoy"
)

// Запасной путь запросов службы через туннель (internal/zapasnoy).
//
// Все походы службы к своим серверам (профиль, /info, вход по коду, наборы
// правил, /logs) и к GitHub (проверка и загрузка обновления) идут через
// klientSeti: сперва напрямую, при сетевом отказе и поднятом туннеле — тем же
// запросом через вход службы в ядре. Разбор журнала ноутбука 19–20.10.2026 —
// в шапке internal/zapasnoy.

// portyVhodaSluzhby — где искать свободный порт для входа службы. Ниже
// динамического диапазона Windows (49152+), чтобы исходящие соединения не
// заняли его между проверкой и стартом ядра; не 2412 (прокси-режим старых
// версий) и не порты Clash API профиля.
var portyVhodaSluzhby = [2]int{20124, 20164}

// zapasnoyPut — один транспорт на службу (пул соединений и память «напрямую
// к этому имени не пройти» общие для всех запросов).
func (s *Sluzhba) zapasnoyPut() *zapasnoy.Put {
	s.zapasOdinRaz.Do(func() {
		s.zapas = &zapasnoy.Put{
			Tunnel: s.adresVhodaSluzhby,
			Adresa: adresaSvoihImen,
		}
	})
	return s.zapas
}

// klientSeti — http.Client службы для походов наружу.
func (s *Sluzhba) klientSeti(srok time.Duration) *http.Client {
	return s.zapasnoyPut().Klient(srok)
}

// adresVhodaSluzhby — куда повторять запрос через туннель; nil — туннель не
// поднят или у живого ядра нет входа службы (принято от старой версии).
func (s *Sluzhba) adresVhodaSluzhby() *url.URL {
	if s.Yadro == nil || s.vykl(vyklZapasnoyPut) {
		return nil
	}
	adres := s.Yadro.VhodSluzhby()
	if adres == "" {
		return nil
	}
	parol := hranenie.SekretClash()
	if parol == "" {
		return nil
	}
	return &url.URL{Scheme: "socks5", User: url.UserPassword(zapasnoy.Polzovatel, parol), Host: adres}
}

// vhodSluzhbyDlyaSborki — вход службы для новой сборки конфига: свободный
// порт сейчас и пароль устройства. nil — вписывать нечего (нет пароля или
// свободного порта), конфиг соберётся как раньше.
//
// Порт выбирается при каждой сборке заново: при плавной смене ядра старое
// ещё держит свой, и новому нужен другой. На отпечаток конфига порт не влияет
// (yadro.OtpechatokKonfigaBayt вход вычёркивает), а живое ядро свой адрес
// помнит само (Yadro.VhodSluzhby, записка передачи).
func vhodSluzhbyDlyaSborki() *konfig.VhodSluzhby {
	parol := hranenie.SekretClash()
	if parol == "" {
		return nil
	}
	port := svobodnyyPort(portyVhodaSluzhby[0], portyVhodaSluzhby[1])
	if port == 0 {
		return nil
	}
	return &konfig.VhodSluzhby{Port: port, Parol: parol}
}

// svobodnyyPort — первый порт из [ot, do), который сейчас можно занять на 127.0.0.1.
func svobodnyyPort(ot, do int) int {
	for port := ot; port < do; port++ {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		l.Close()
		return port
	}
	return 0
}

// adresaSvoihImen — адреса имени, зашитые сервером в профиль (dns hosts).
// Читается с диска на каждый отказ DNS: дёшево, и профиль всегда последний.
func adresaSvoihImen(host string) []string {
	b, err := os.ReadFile(hranenie.PutProfilya())
	if err != nil {
		return nil
	}
	return zapasnoy.ImenaIzProfilya(b)[host]
}
