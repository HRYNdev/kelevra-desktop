package proksi

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
)

// С 0.6.77 Kelevra системный прокси НЕ СТАВИТ НИКОГДА: неполного режима больше
// нет (решение владельца 17.10), и функций, которые прокси прописывают, в
// пакете нет вовсе — запрет держится конструкцией, а не словами. Пакет
// остался ради одного дела: убрать прокси, оставленный СТАРЫМИ версиями,
// у тех, кто обновился из прокси-режима (UbratSled).

// StaryyAdres — адрес, на который прокси-режим старых версий ставил системный
// прокси: mixed-вход боевого профиля. По нему уборка узнаёт свою запись, даже
// если метка до нас не дожила (жёсткая смерть до записи метки, метка в другой
// папке).
const StaryyAdres = "127.0.0.1:2412"

// imyaMetki — файл-метка «системный прокси на этот адрес поставили мы».
// Писали её старые версии; новые только читают и убирают.
const imyaMetki = "proksi.json"

type metka struct {
	Adres string `json:"adres"`
}

// papkiMetok — где может лежать метка старой версии. Две папки, а не одна:
// при переезде данных в общую папку службы Windows (hranenie.Perenesti) метка
// НЕ переносится и остаётся в профиле человека, а hranenie.Papka() после
// переезда смотрит уже в общую.
func papkiMetok() []string {
	var papki []string
	for _, p := range []string{hranenie.Papka(), hranenie.PapkaLichnaya()} {
		if p == "" {
			continue
		}
		povtor := false
		for _, est := range papki {
			if est == p {
				povtor = true
			}
		}
		if !povtor {
			papki = append(papki, p)
		}
	}
	return papki
}

// ProchestMetku читает метку, оставленную старой версией. Пустой адрес и
// false — метки нет ни в одной из папок.
func ProchestMetku() (string, bool) {
	for _, p := range papkiMetok() {
		b, err := os.ReadFile(filepath.Join(p, imyaMetki))
		if err != nil {
			continue
		}
		var m metka
		if err := json.Unmarshal(b, &m); err != nil || m.Adres == "" {
			continue
		}
		return m.Adres, true
	}
	return "", false
}

// UbratMetku удаляет метку во всех папках, где она может лежать.
func UbratMetku() {
	for _, p := range papkiMetok() {
		_ = os.Remove(filepath.Join(p, imyaMetki))
	}
}

// nashiAdresa — какие адреса в реестре уборка считает своими: из метки (если
// она есть) и StaryyAdres.
func nashiAdresa() []string {
	adresa := []string{StaryyAdres}
	if a, est := ProchestMetku(); est && a != StaryyAdres {
		adresa = append(adresa, a)
	}
	return adresa
}
