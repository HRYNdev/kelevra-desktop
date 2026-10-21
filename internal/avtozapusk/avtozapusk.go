// Пакет avtozapusk включает и выключает запуск Kelevra вместе с Windows.
//
// Зачем это вообще. Защита, которую человек обязан включать руками после
// каждой перезагрузки, выключена ровно столько, сколько он про неё не вспомнил.
// Автозапуск поднимает СЛУЖБУ (`--tiho`: ядро и прокси без окна), а не окно:
// приложение, которое само лезет на экран при каждом включении компьютера,
// выключают первым же делом.
package avtozapusk

import (
	"regexp"
	"strings"
)

// Imya — под этим именем запись видна человеку в диспетчере задач,
// на вкладке «Автозагрузка». Менять его нельзя: старая запись останется
// висеть, и Kelevra будет запускаться дважды.
const Imya = "Kelevra"

// Argument — режим, в котором приложение стартует вместе с системой.
const Argument = "--tiho"

// ArgumentSoSluzhboy — режим автозапуска, когда туннель держит служба Windows.
//
// Разница существенная. Без службы запись автозапуска поднимает всё
// приложение: ядро, туннель и значок разом. Со службой ядро уже поднято
// системой задолго до входа человека, и поднимать его второй раз нельзя — за
// адаптер и порт подерутся два хозяина. Человеку тогда нужен только значок,
// он же и открывает окно.
const ArgumentSoSluzhboy = "--trey"

// Nuzhnyy отвечает, какой из двух режимов записать в автозапуск. Решение
// принимает вызывающий: пакет автозапуска про службы ничего не знает и знать
// не должен, его дело — реестр.
func Nuzhnyy(estSluzhba bool) string {
	if estSluzhba {
		return ArgumentSoSluzhboy
	}
	return Argument
}

// Починка записи на копию в Program Files (21.10.2026).
//
// Беда на ПК Вовы: запись смотрела на `Downloads\Kelevra.exe --trey`, а файл
// удалили 18.10 — с тех пор при входе в Windows значок трея не поднимался
// ни разу, и служба каждый день писала «окно трея не поднято». Пишет запись
// только тумблер в окне (Vklyuchit — путь того exe, что его обслуживает), а
// после переезда службы в Program Files прежняя запись остаётся смотреть
// туда, где Kelevra когда-то запускали руками.
//
// Поэтому окно и значок при каждом старте сверяют запись с копией в
// Program Files — той, которую держит служба и которую не удалит уборка
// «Загрузок». Решение — чистая функция Pochinka, исполнение — в
// avtozapusk_windows.go.

// PutIzZapisi достаёт путь exe из строки автозапуска: `"C:\A B\Kelevra.exe"
// --trey` → `C:\A B\Kelevra.exe`. Без кавычек путь — всё до «.exe»
// включительно (старые записи и ручные правки), иначе до первого пробела.
func PutIzZapisi(zapis string) string {
	z := strings.TrimSpace(zapis)
	if z == "" {
		return ""
	}
	if z[0] == '"' {
		if konec := strings.IndexByte(z[1:], '"'); konec >= 0 {
			return z[1 : 1+konec]
		}
		return strings.TrimPrefix(z, `"`)
	}
	if i := strings.Index(strings.ToLower(z), ".exe"); i >= 0 {
		return z[:i+len(".exe")]
	}
	if i := strings.IndexByte(z, ' '); i >= 0 {
		return z[:i]
	}
	return z
}

// StrokaDlya — строка автозапуска для exe put в режиме argument. Путь в
// кавычках: в «Program Files» есть пробел.
func StrokaDlya(put, argument string) string {
	return `"` + put + `" ` + argument
}

// Pochinka решает, переписать ли запись автозапуска на копию в Program Files.
//
//	zapis, estZapis — что сейчас лежит в реестре (estZapis=false — записи нет);
//	putPF           — exe в Program Files, "" — его там нет;
//	argument        — нужный режим (Nuzhnyy).
//
// Переписываем, когда запись есть, а смотрит не туда: другой путь (в том
// числе на удалённый файл в «Загрузках») или не тот режим. Не трогаем, когда:
//   - записи нет или она пустая — человек сам выключил автозапуск, и
//     включать его за спиной нельзя;
//   - копии в Program Files нет — переписать не на что;
//   - запись уже верная (регистр и пробелы по краям не в счёт: на Windows
//     C:\Program Files и c:\program files одно и то же место).
func Pochinka(zapis string, estZapis bool, putPF, argument string) (string, bool) {
	if !estZapis || strings.TrimSpace(zapis) == "" || putPF == "" || argument == "" {
		return "", false
	}
	nado := StrokaDlya(putPF, argument)
	if strings.EqualFold(strings.TrimSpace(zapis), nado) {
		return "", false
	}
	return nado, true
}

// Zapis — запись автозапуска одного пользователя машины (для диагностики).
type Zapis struct {
	Stroka  string // как лежит в реестре
	Put     string // exe из неё (PutIzZapisi)
	FaylEst bool   // файл по этому пути есть на диске
}

// profil — начало пути внутри профиля пользователя: C:\Users\<имя>\.
var profil = regexp.MustCompile(`(?i)^[a-z]:\\users\\[^\\]+\\`)

// OpisanieDlyaDiagnostiki — записи всех пользователей одной строкой для
// diagnostika пульта: «<путь> [est|net]» через запятую, «net_zapisi» —
// записи нет ни у кого. Имя учётки Windows в пути заменено на
// %USERPROFILE%: для разбора важно «Загрузки или Program Files», а не имя.
// Без «;» и «=»: строка встаёт значением в «ключ=значение; …».
func OpisanieDlyaDiagnostiki(zapisi []Zapis) string {
	if len(zapisi) == 0 {
		return "net_zapisi"
	}
	chasti := make([]string, 0, len(zapisi))
	for _, z := range zapisi {
		put := z.Put
		if put == "" {
			put = z.Stroka
		}
		put = profil.ReplaceAllString(put, `%USERPROFILE%\`)
		put = strings.NewReplacer(";", ",", "=", "-").Replace(put)
		fayl := "net"
		if z.FaylEst {
			fayl = "est"
		}
		chasti = append(chasti, put+" ["+fayl+"]")
	}
	return strings.Join(chasti, ", ")
}
