//go:build windows

package ustroystvo

import (
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Реестр, а не запуск wmic/powershell. WMI отдаёт Win32_ComputerSystem.Manufacturer
// и Win32_BaseBoard.Product из SMBIOS, а Windows кладёт те же поля SMBIOS в
// HARDWARE\DESCRIPTION\System\BIOS ещё на старте — читаются они обычным
// пользователем, без прав и без порождения процесса. Порождать процесс тут
// нельзя вдвойне: Kelevra.exe оконный (-H windowsgui), и каждый wmic — это
// мелькнувшее чёрное окно консоли у человека на экране.
const putBIOS = `HARDWARE\DESCRIPTION\System\BIOS`

// putVersiiWindows — где Windows держит своё название, номер сборки и
// «22H2». Тот же ключ, что показывает winver.
const putVersiiWindows = `SOFTWARE\Microsoft\Windows NT\CurrentVersion`

func modelSistemy() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, putBIOS, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	proizvoditel, izdelie := stroka(k, "SystemManufacturer"), stroka(k, "SystemProductName")
	if musor(proizvoditel) || musor(izdelie) {
		// Настольная сборка: «система» в SMBIOS не заполнена вовсе (там стоят
		// «System manufacturer»/«System Product Name» из примера), а правду
		// знает материнская плата. Это как раз случай из заказа — «ASUS TUF
		// Gaming B550» это имя ПЛАТЫ, а не готового изделия.
		if p, i := stroka(k, "BaseBoardManufacturer"), stroka(k, "BaseBoardProduct"); !musor(i) {
			proizvoditel, izdelie = p, i
		}
	}
	if musor(proizvoditel) {
		proizvoditel = ""
	}
	if musor(izdelie) {
		izdelie = ""
	}
	return sobrat(korotkiyProizvoditel(proizvoditel), izdelie)
}

func platformaSistemy() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, putVersiiWindows, registry.QUERY_VALUE)
	if err != nil {
		return ZapasnayaPlatforma
	}
	defer k.Close()
	sborka := 0
	if s := stroka(k, "CurrentBuildNumber"); s != "" {
		sborka, _ = strconv.Atoi(s)
	}
	// ProductName на Windows 11 до сих пор говорит «Windows 10 Pro» — это
	// известная и НЕ исправленная Microsoft вещь. Поколение поэтому берём по
	// номеру сборки (22000 — первая одиннадцатая), а из ProductName оставляем
	// только редакцию: Pro, Home, Education.
	imya := ZapasnayaPlatforma
	switch {
	case sborka >= 22000:
		imya = "Windows 11"
	case sborka >= 10240:
		imya = "Windows 10"
	}
	if r := redakciya(stroka(k, "ProductName")); r != "" {
		imya += " " + r
	}
	if v := stroka(k, "DisplayVersion"); v != "" {
		imya += " " + v
	} else if v := stroka(k, "ReleaseId"); v != "" {
		// До 20H2 «22H2»-строки не было вовсе, версия жила в ReleaseId.
		imya += " " + v
	}
	if sborka > 0 {
		imya += " (" + strconv.Itoa(sborka) + ")"
	}
	return imya
}

// redakciya вынимает из «Windows 10 Pro» слово «Pro»: номер поколения уже
// сказан по сборке, а слово «Windows» в строке платформы и так есть.
func redakciya(produkt string) string {
	var slova []string
	for _, s := range strings.Fields(produkt) {
		nizhniy := strings.ToLower(s)
		if nizhniy == "windows" || nizhniy == "microsoft" {
			continue
		}
		if _, err := strconv.Atoi(s); err == nil {
			continue
		}
		slova = append(slova, s)
	}
	return strings.Join(slova, " ")
}

// stroka читает строковое значение и молчит про любой отказ: отсутствующее
// поле в SMBIOS — обычное дело, а не повод остаться вовсе без модели.
func stroka(k registry.Key, imya string) string {
	v, _, err := k.GetStringValue(imya)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}
