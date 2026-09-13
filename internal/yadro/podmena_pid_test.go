package yadro

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

// TestChuzhoyMertvyPidOtvergaetsya — КОНТРОЛЬ для теста ниже. Если опознание
// не отличает мёртвый PID от живого чужого, это не беда продукта, а слепота
// самого прибора (замер бессмыслен). PID заведомо за пределами реального
// пространства процессов.
// Контроль имеет смысл только на целевой ОС: на Unix os.FindProcess успешен
// ВСЕГДА (см. go doc os.FindProcess), а /proc/<pid>/exe у несуществующего
// процесса не читается — то есть «не узнали», и по правилу «молчание ОС не
// повод рвать туннель» отказывать тут нечем. Это не слабость прибора, а
// осознанная семантика продукта, поэтому на не-Windows контроль пропускается
// явно, а не подгоняется под зелёный.
func TestChuzhoyMertvyPidOtvergaetsya(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("контроль по мёртвому PID осмыслен только на Windows: на Unix FindProcess всегда успешен, а образа мёртвого процесса ОС не знает")
	}
	papka := t.TempDir()
	y := yadroNaPodstavnomApi(t, papka, true)

	itog := y.Prinyat(Peredacha{
		PID: 99999999, // заведомо мёртвый/несуществующий
		Bin: y.Bin,
		Api: y.Api,
	}, "")

	if itog.Prinyato {
		t.Fatalf("КОНТРОЛЬ ПРОВАЛЕН: мёртвый PID принят как живой (itog=%+v) — "+
			"прибор не различает мёртвый/живой PID, тест ниже недоказателен", itog)
	}
	t.Logf("контроль пройден: мёртвый PID отвергнут, Pochemu=%q", itog.Pochemu)
}

// TestChuzhoyZhivoyPidNeOpoznaetsyaSvoim — регрессия на беду наряда
// 0912-220306: раньше opoзнание сверяло ТОЛЬКО строку p.Bin со строкой
// y.Bin (обе — из конфига, не из ОС), поэтому живой ПОСТОРОННИЙ процесс,
// переиспользовавший PID прошлой копии, принимался как своё ядро. Теперь
// Prinyat спрашивает у ОС реальный путь образа процесса с этим PID
// (putObrazaProcessa) и сверяет его с y.Bin: посторонний образ обязан
// отвергаться.
func TestChuzhoyZhivoyPidNeOpoznaetsyaSvoim(t *testing.T) {
	papka := t.TempDir()
	y := yadroNaPodstavnomApi(t, papka, true) // порт отвечает 200 — "живое ядро"

	// Настоящий живой процесс, который к sing-box не имеет никакого
	// отношения. Его PID мог достаться переиспользованному номеру после
	// перезагрузки/падения прошлой копии Kelevra. ping.exe — штатная часть
	// Windows/wine.
	postoronniy := exec.Command("ping", "-n", "30", "127.0.0.1")
	if err := postoronniy.Start(); err != nil {
		t.Fatalf("не смог поднять посторонний живой процесс: %v", err)
	}
	t.Cleanup(func() { _ = postoronniy.Process.Kill() })
	chuzhoyPid := postoronniy.Process.Pid

	if _, znaem := putObrazaProcessa(chuzhoyPid); !znaem {
		t.Skip("ОС не назвала путь образа постороннего процесса на этой платформе/сборке — опознание по образу здесь недоступно, тест недоказателен")
	}

	itog := y.Prinyat(Peredacha{
		PID: chuzhoyPid, // ЧУЖОЙ живой процесс, не sing-box
		Bin: y.Bin,      // строка совпадает — это статичный путь из конфига, не из ОС
		Api: y.Api,
	}, "")

	if itog.Prinyato {
		t.Fatalf("БЕДА ВЕРНУЛАСЬ: посторонний живой процесс (PID %d, реально 'ping') "+
			"принят как прошлая копия ядра Kelevra: itog=%+v", chuzhoyPid, itog)
	}
	t.Logf("беда починена: посторонний живой процесс отвергнут, Pochemu=%q", itog.Pochemu)
}

// TestSvoyoZhivoePoObrazuPrinimaetsya — вторая половина доказательства:
// правка не должна начать отвергать ВСЁ подряд. Берём взаправду запущенный
// процесс (этот же тестовый .exe) и подставляем его НАСТОЯЩИЙ путь образа,
// каким его называет ОС, в y.Bin — то есть ровно то, с чем сверяет
// putObrazaProcessa. Такое ядро обязано приниматься.
func TestSvoyoZhivoePoObrazuPrinimaetsya(t *testing.T) {
	papka := t.TempDir()
	y := yadroNaPodstavnomApi(t, papka, true)

	svoyPid := os.Getpid()
	obraz, znaem := putObrazaProcessa(svoyPid)
	if !znaem {
		t.Skip("ОС не назвала путь собственного образа на этой платформе/сборке — опознание по образу здесь недоступно, тест недоказателен")
	}
	y.Bin = obraz

	itog := y.Prinyat(Peredacha{
		PID: svoyPid,
		Bin: y.Bin,
		Api: y.Api,
	}, "")

	if !itog.Prinyato {
		t.Fatalf("НЕ беда: своё же ядро (образ процесса совпадает с y.Bin) отвергнуто: %s", itog.Pochemu)
	}
	t.Logf("своё ядро по-прежнему принимается: itog=%+v", itog)
}
