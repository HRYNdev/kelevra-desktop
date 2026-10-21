package ustroystvo

import (
	"net/http"
	"testing"
)

func TestZagolovkiPultaISostoyaniya(t *testing.T) {
	h := http.Header{}
	ZagolovokPulta(h, "v=1;umeet=1;versiya=3;kvit=k1:ok")
	ZagolovokSostoyaniya(h, "avto=doma;tunnel=0\r\nX-Zlo: 1")
	if h.Get("X-Pult") != "v=1;umeet=1;versiya=3;kvit=k1:ok" {
		t.Fatalf("X-Pult %q", h.Get("X-Pult"))
	}
	if h.Get("X-Sostoyanie") != "avto=doma;tunnel=0 X-Zlo: 1" {
		t.Fatalf("перевод строки дошёл до заголовка: %q", h.Get("X-Sostoyanie"))
	}
	h2 := http.Header{}
	ZagolovokPulta(h2, "")
	ZagolovokSostoyaniya(h2, "")
	if len(h2) != 0 {
		t.Fatal("пустые значения поставили заголовки")
	}
	// Адресат пульта сверяется ровно с тем, что уходит в X-Device-Id.
	h3 := http.Header{}
	Zagolovki(h3, " id\tс кириллицей ", "1")
	if IdVZagolovke(" id\tс кириллицей ") != h3.Get("X-Device-Id") {
		t.Fatal("IdVZagolovke разошёлся с X-Device-Id")
	}
}
