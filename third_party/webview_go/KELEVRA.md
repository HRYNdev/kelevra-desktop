Копия github.com/webview/webview_go v0.0.0-20240831120633-6173450d4dd6.

Единственная правка — `libs/webview/include/webview.h`, `get_webkit_library()`:
в списке имён первыми стоят `libwebkit2gtk-4.1.so.0` и `libwebkit2gtk-4.0.so.37`.
Исходник искал только `libwebkit2gtk-4.1.so` / `-4.0.so` (`dlopen(..., RTLD_NOLOAD)`),
а эти безверсионные имена ставит лишь пакет `-dev`. Без него `eval` молча ничего
не делал: ответы привязок (`w.Bind`) не доходили до страницы, `await` висел,
окно Linux оставалось пустым (стенд 314, 20.11.2026).
