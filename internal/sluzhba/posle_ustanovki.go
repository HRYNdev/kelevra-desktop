package sluzhba

import (
	"context"
	"log"

	"github.com/HRYNdev/kelevra-desktop/internal/hranenie"
)

// PodklyuchitPosleUstanovki — первый старт службы Windows после установки:
// человек только что дал права (при первом запуске, кнопкой «Дать права» или
// после обновления со старой версии), и связь должна подняться сама — «скачал,
// дал права админа, ВСЁ, РАБОТАЕТ» (решение владельца 17.10).
//
// Подключается тем же путём, что нажатие «Подключиться» (Podklyuchit): дома
// при включённом авторежиме туннель не поднимается — обход делает роутер.
// Кода доступа нет (чистая установка) — подключать нечем, флаг просто снят.
func (s *Sluzhba) PodklyuchitPosleUstanovki(ctx context.Context) {
	if !hranenie.ZabratFlagPodklyuchit() {
		return
	}
	if s.Nastroyki.Kod == "" && s.Nastroyki.Podpiska == "" {
		log.Printf("служба только что установлена; кода доступа ещё нет — подключать нечем")
		return
	}
	log.Printf("служба только что установлена по просьбе человека — подключаю")
	if err := s.Podklyuchit(ctx); err != nil {
		log.Printf("подключение после установки службы не удалось: %v", err)
	}
}
