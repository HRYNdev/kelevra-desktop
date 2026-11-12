package yadro

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Снимок конфига, по которому работает живое ядро.
//
// Зачем. config.json на диске — не то, по чему работает ядро: ежечасная
// пересборка профиля пишет его, не трогая ядро, а подъём пишет его с
// подобранным именем адаптера и правилами из кеша. Решить, нужен ли
// перезапуск (SravnitKonfigi), можно только сравнив новый конфиг с тем, что
// ядро на самом деле прочитало на старте. Его и кладёт Zapustit, когда ядро
// ответило: рядом с запиской передачи, закрыто (в нём пароль Clash API и
// ключи узлов). Снимок переживает смену версии вместе с ядром, по нему же
// судит принимающая копия.
const imyaSnimka = "konfig_v_rabote.json"

func putSnimka(papka string) string { return filepath.Join(papka, imyaSnimka) }

func zapisatSnimok(papka string, telo []byte) {
	if len(telo) == 0 {
		return
	}
	_ = zapisatZakryto(putSnimka(papka), telo)
}

func ubratSnimok(papka string) { _ = os.Remove(putSnimka(papka)) }

// KonfigRaboty — что известно о конфиге живого ядра.
type KonfigRaboty struct {
	// Telo — сам конфиг (снимок); nil — снимка нет или он не от этого ядра
	// (ядро поднято версией до 07.11.2026).
	Telo []byte
	// Otpechatok — отпечаток конфига живого ядра (из записки или старта);
	// пусто — не знаем.
	Otpechatok string
}

// KonfigVRabote — конфиг, по которому работает ядро этой копии. ok=false —
// ядро не работает.
func (y *Yadro) KonfigVRabote() (KonfigRaboty, bool) {
	y.zamok.Lock()
	rabotaet := y.derzh != nil
	otp := y.otpechatokRaboty
	y.zamok.Unlock()
	if !rabotaet {
		return KonfigRaboty{}, false
	}
	return SnimokDlyaOtpechatka(y.Papka, otp), true
}

// SnimokDlyaOtpechatka — снимок из папки ядра, если он от конфига с этим
// отпечатком (записка передачи). Для приёма живого ядра: Yadro к этому
// моменту ядро ещё не держит.
func SnimokDlyaOtpechatka(papka, otpechatok string) KonfigRaboty {
	k := KonfigRaboty{Otpechatok: otpechatok}
	if otpechatok == "" {
		return k
	}
	telo, err := os.ReadFile(putSnimka(papka))
	if err != nil || OtpechatokKonfigaBayt(telo) != otpechatok {
		return k
	}
	k.Telo = telo
	return k
}

// imyaAdapteraIzBayt — ImyaAdapteraIzKonfiga по уже прочитанным байтам.
func imyaAdapteraIzBayt(b []byte) string {
	var derevo struct {
		Inbounds []struct {
			InterfaceName string `json:"interface_name"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(b, &derevo); err != nil {
		return ""
	}
	for _, v := range derevo.Inbounds {
		if v.InterfaceName != "" {
			return v.InterfaceName
		}
	}
	return ""
}
