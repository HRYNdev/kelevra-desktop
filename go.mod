module github.com/HRYNdev/kelevra-desktop

go 1.24

require (
	fyne.io/systray v1.12.2
	github.com/jchv/go-webview2 v0.0.0-20260205173254-56598839c808
	github.com/webview/webview_go v0.0.0-20240831120633-6173450d4dd6
	golang.org/x/sys v0.15.0
	rsc.io/qr v0.2.0
)

require (
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect
)

// Копия webview_go с одной правкой (third_party/webview_go/KELEVRA.md):
// библиотека WebKitGTK ищется по имени с версией, а не только по имени из -dev.
replace github.com/webview/webview_go => ./third_party/webview_go
