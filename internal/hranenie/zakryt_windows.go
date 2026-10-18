//go:build windows

package hranenie

import "golang.org/x/sys/windows"

// sddlTolkoSistema — доступ к файлу только у системы (SY) и администраторов
// (BA), без наследования от папки («P»). Папка %PROGRAMDATA%\Kelevra даёт
// обычным пользователям чтение (vinsluzhba.zakrytPapku): окну нужен журнал,
// а пароли и конфиг ядра с паролем читать им незачем — их читает только
// служба, она работает от системы.
const sddlTolkoSistema = "D:P(A;;FA;;;SY)(A;;FA;;;BA)"

// zakrytFayl ставит файлу права «только система и администраторы», снимая
// унаследованные. Уже открытые дескрипторы (в том числе наш, на запись) от
// этого не теряют доступа.
func zakrytFayl(put string) error {
	sd, err := windows.SecurityDescriptorFromString(sddlTolkoSistema)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(put, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil)
}
