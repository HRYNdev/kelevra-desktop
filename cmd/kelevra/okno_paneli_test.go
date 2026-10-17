package main

import "testing"

func TestSsylkaPaneli(t *testing.T) {
	if _, ok := ssylkaPaneliIzArgov([]string{"--panel", "https://node2kv.example/panel/vhod/abc"}); !ok {
		t.Fatal("настоящая ссылка панели отвергнута")
	}
	for _, plohaya := range []string{"http://node2kv.example/panel/vhod/abc", "https://evil.example/", "file:///C:/x", "javascript:alert(1)"} {
		if _, ok := ssylkaPaneliIzArgov([]string{"--panel", plohaya}); ok {
			t.Errorf("пропущена не ссылка панели: %s", plohaya)
		}
	}
	if _, ok := ssylkaPaneliIzArgov([]string{"--trey"}); ok {
		t.Fatal("без --panel окна панели быть не должно")
	}
}
