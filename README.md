# Texora

Свой текстовый редактор под Windows (аналог Word) на Go: только стандартная библиотека, без cgo, один exe, работает без интернета.
Форматы docx, odt, rtf, doc (чтение), html, pdf (экспорт и читалка). Упор на русские учебные работы: шаблоны и библиография по ГОСТ,
оглавление, названия и перекрёстные ссылки, формулы, орфография ru/en.

## Установка

Скачайте **Texora.exe** со страницы [последнего релиза](https://github.com/FallenKeK/Texora/releases/latest) и запустите —
установка не нужна. Exe пока без цифровой подписи: если Windows покажет «Windows защитила ваш компьютер», нажмите
«Подробнее» → «Выполнить в любом случае». Новые версии Texora находит и ставит сама (раз в день, можно выключить в настройках).

- Подробности, решения и история версий — [ARCHITECTURE.md](ARCHITECTURE.md).
- Сводка для внешней консультации — [texora_brief_for_gpt.md](texora_brief_for_gpt.md).

## Сборка

```sh
GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui -s -w" -o Texora.exe .
```

Значок, манифест и версия exe лежат в `rsrc_windows_amd64.syso`; после смены `AppVersion` (ui.go):

```sh
go run . mkres
cd assets && llvm-windres -c 65001 --target=pe-x86-64 -i app.rc -O coff -o ../rsrc_windows_amd64.syso
```

## Проверка

```sh
go test ./...          # автотесты (на Linux — со сценариями правки и кругового сохранения)
go run . interactive   # и другие сценарии: features, captions, eqtest, scenario, pdftest, perf — см. ARCHITECTURE.md
```

На Linux программа не открывает окно, а рисует интерфейс в PNG (ttf.go), поэтому её можно проверять без Windows.
CI (`.github/workflows`) на windows-latest собирает exe, запускает его, снимает скриншоты и гоняет тесты.

## Лицензия

[MIT](LICENSE). Вшитые словари орфографии и переносов взяты из LibreOffice и распространяются на своих условиях —
см. [NOTICES.md](NOTICES.md).
