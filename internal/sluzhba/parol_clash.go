package sluzhba

import (
	"log"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
)

// smenitStaryyParolPeredStartom меняет пароль Clash API, лежавший до 0.6.80
// открытым для всех пользователей ПК, — в тот момент, когда ядро и так
// запускается заново (PodnyatZashchitu: «Подключиться», выход из дома при
// авторежиме, подъём после перезагрузки), до сборки его конфига.
//
// Только при незапущенном ядре: работающее ядро держит пароль из своего
// конфига, и смена под ним дала бы 401 на каждом запросе службы к ядру
// (замеры, выбор узла, счётчик трафика) до его перезапуска. Принятое после
// обновления живое ядро (PrinyatZhivoeYadro) работает со старым паролем до
// своей ближайшей остановки — туннель ради смены пароля не рвётся.
func (s *Sluzhba) smenitStaryyParolPeredStartom() {
	if s.Yadro.PID() != "" {
		return
	}
	smenen, err := hranenie.SmenitStaryySekret()
	if err != nil {
		log.Printf("пароль Clash API прежний (смена не удалась, повторю при следующем запуске ядра): %v", err)
		return
	}
	if smenen {
		log.Printf("пароль Clash API заменён новым перед запуском ядра (прежний создан до закрытия файла)")
	}
}
