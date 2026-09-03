# PrinterStats

Go-программа определяет сетевые принтеры и собирает метрики по SNMP:

- общий счётчик страниц;
- остаток тонера, чернил или термотрансферной ленты;
- напечатанную длину для поддерживаемых термопринтеров TSC;
- модель, имя устройства, SNMP-версию и способ определения принтера;
- подробную диагностику недоступных и нестандартных устройств.

Локальные права администратора не нужны. На устройстве должен быть включён SNMP, компьютер должен иметь доступ к UDP/161, а community должна иметь права чтения.

## Запуск списка из printers.json

Из каталога, где находится go.mod:

~~~powershell
go run ./cmd/printerstats -config .\printers.json
~~~

Сохранение полного результата:

~~~powershell
go run ./cmd/printerstats -config .\printers.json -format json -out .\results.json
go run ./cmd/printerstats -config .\printers.json -format csv -out .\results.csv
~~~

## Проверка одного IP

Файл конфигурации не требуется:

~~~powershell
go run ./cmd/printerstats -ip 192.168.140.30
~~~

По умолчанию программа пробует SNMP v2c, затем v1:

~~~powershell
go run ./cmd/printerstats -ip 192.168.40.32 -version auto -format json
~~~

Если community отличается от public:

~~~powershell
go run ./cmd/printerstats -ip 192.168.140.30 -community read-only-community
~~~

## Диагностический обход vendor-OID

Программа может определить корень enterprise-OID из sysObjectID и приложить до 500 значений к JSON:

~~~powershell
go run ./cmd/printerstats -ip 192.168.140.34 -version 2c -format json -walk-oid auto -out .\kyocera-oids.json
~~~

Явный корень:

~~~powershell
go run ./cmd/printerstats -ip 192.168.40.32 -version 1 -format json -walk-oid .1.3.6.1.4.1.43564 -out .\tsc-oids.json
~~~

Диагностический обход только читает OID и ничего не изменяет на принтере.

## Интерпретация таблицы

- PRINTER=yes — устройство подтверждено как принтер.
- PRINTER=no — SNMP ответил, но признаков принтера нет.
- PRINTER=unknown — SNMP не ответил, поэтому определить устройство невозможно.
- PAGES — счётчик листов/отпечатков.
- LENGTH_KM — напечатанная длина термопринтера.
- SUPPLY% — минимальный остаток основного расходника: тонера, чернил или ленты.
- SNMP=2c,1 — программа попробовала обе версии.

Для TSC TTP-2410MT/MH240 используется 8 точек/мм, для MH341 — 12 точек/мм. Прошивки TSC могут объявлять ёмкость ленты в микрометрах, но текущий уровень возвращать как целый процент; для этого ограниченного случая применяется vendor-коррекция.

## Сборка EXE

~~~powershell
go build -o printerstats.exe ./cmd/printerstats
.\printerstats.exe -config .\printers.json
~~~

## Ошибки

- request timeout — устройство выключено, SNMP отключён, неверная community, закрыт UDP/161 или нет маршрута.
- invalid packet length — старая прошивка/принт-сервер сформировали некорректный пакет. Программа автоматически повторяет запросы метаданных по одному OID.
- partial — принтер определён, но часть стандартных метрик недоступна.

Даже при ошибках отдельных устройств доступные результаты сохраняются. Если хотя бы один включённый адрес завершился с error, процесс возвращает код 1.
