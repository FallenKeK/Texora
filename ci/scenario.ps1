. .\ci\lib.ps1
$p = Start-Tx testdata\feat.docx
Keys "n"; Start-Sleep 3            # «Установить Texora?» — нет
Shot "t0_open"
Keys "^n"; Start-Sleep 2
Click 600 400
Keys "Привет мир. Это тест таблиц."
Keys "{ENTER}"
Keys "^k"; Keys "табл"; Shot "t1_palette"; Keys "{ESC}"
Cmd "Таблица с параметрами"; Shot "t2_dialog"
Keys "{ENTER}"; Start-Sleep 1
Shot "t3_inserted"
Keys "Ячейка1{TAB}Ячейка2{TAB}Ячейка3{TAB}"
Shot "t4_filled"
$ops = @("Вставить строку снизу","Вставить строку сверху","Вставить столбец справа","Вставить столбец слева","Объединить с ячейкой",
         "Удалить строку","Удалить столбец","Границы","Заливка ячейки","Удалить таблицу")
$i=0
foreach ($o in $ops) { $i++; Cmd $o; Shot ("tb_$i") }
Keys "^z"; Keys "^z"
Cmd "Тема: Светлая"; Shot "t5_light"
Cmd "Тема: Графит"
Shot "t6_end"
"alive=$(-not $p.HasExited)" | Add-Content out\log.txt
Copy-Item "$env:TEMP\Texora-crash.txt" out -ErrorAction SilentlyContinue
Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
