//go:build windows

package main

// Установка без интернета: копия в %LOCALAPPDATA%\Programs\Texora, ярлыки, «Открыть с помощью», удаление.

import (
	_ "embed"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

//go:embed assets/Texora.ico
var iconICO []byte

const appName = "Texora"

var version = AppVersion

const (
	mbOK          = 0x0
	mbYesNoCancel = 0x3
	mbYesNo       = 0x4
	mbIconInfo    = 0x40
	mbIconQ       = 0x20
	mbTopmost     = 0x40000
	idYes         = 6
	idNo          = 7
)

func messageBox(title, text string, flags uint32) int {
	return int(call(pMessageBoxW, 0, uintptr(unsafe.Pointer(wstr(text))), uintptr(unsafe.Pointer(wstr(title))), uintptr(flags|mbTopmost)))
}

func showInfo(title, text string) { messageBox(title, text, mbOK|mbIconInfo) }

func hidden(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd
}

func localAppData() string {
	if v := os.Getenv("LOCALAPPDATA"); v != "" {
		return v
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "AppData", "Local")
}

func installDir() string { return filepath.Join(localAppData(), "Programs", appName) }
func dataDir() string    { return filepath.Join(localAppData(), appName) }

// ---------------------------------------------------------------- установка
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func runPowerShell(script string) error {
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		b[2*i] = byte(v)
		b[2*i+1] = byte(v >> 8)
	}
	enc := base64.StdEncoding.EncodeToString(b)
	return hidden(exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", enc)).Run()
}

func reg(args ...string) {
	_ = hidden(exec.Command("reg", append([]string{"add"}, append(args, "/f")...)...)).Run()
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}

func install() (string, error) {
	dir := installDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	self, _ := os.Executable()
	exe := filepath.Join(dir, appName+".exe")
	if !strings.EqualFold(filepath.Clean(self), filepath.Clean(exe)) {
		if err := copyFile(self, exe); err != nil {
			return "", err
		}
	}
	removeLegacy()
	ico := filepath.Join(dir, "Texora.ico")
	_ = os.WriteFile(ico, iconICO, 0o644)

	script := `$ws = New-Object -ComObject WScript.Shell
$exe = ` + psQuote(exe) + `
$ico = ` + psQuote(ico) + `
foreach ($dir in @([Environment]::GetFolderPath('Desktop'), [Environment]::GetFolderPath('Programs'))) {
  $s = $ws.CreateShortcut((Join-Path $dir 'Texora.lnk'))
  $s.TargetPath = $exe; $s.IconLocation = $ico; $s.Description = 'Texora — текстовый редактор'
  $s.WorkingDirectory = [Environment]::GetFolderPath('MyDocuments'); $s.Save()
}
$u = $ws.CreateShortcut((Join-Path ([Environment]::GetFolderPath('Programs')) 'Удалить Texora.lnk'))
$u.TargetPath = $exe; $u.Arguments = '--uninstall'; $u.IconLocation = $ico; $u.Save()`
	_ = runPowerShell(script)

	open := `"` + exe + `" "%1"`
	reg(`HKCU\Software\Classes\Texora.Document`, "/ve", "/d", "Документ Texora")
	reg(`HKCU\Software\Classes\Texora.Document\DefaultIcon`, "/ve", "/d", ico)
	reg(`HKCU\Software\Classes\Texora.Document\shell\open\command`, "/ve", "/d", open)
	reg(`HKCU\Software\Classes\Applications\Texora.exe\shell\open\command`, "/ve", "/d", open)
	reg(`HKCU\Software\Classes\Applications\Texora.exe`, "/v", "FriendlyAppName", "/d", "Texora")
	for _, ext := range []string{".docx", ".doc", ".odt", ".rtf", ".txt", ".pdf", ".pptx"} {
		reg(`HKCU\Software\Classes\`+ext+`\OpenWithProgids`, "/v", "Texora.Document", "/t", "REG_NONE")
		reg(`HKCU\Software\Classes\Applications\Texora.exe\SupportedTypes`, "/v", ext, "/t", "REG_SZ", "/d", "")
	}
	un := `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\Texora`
	reg(un, "/v", "DisplayName", "/d", "Texora")
	reg(un, "/v", "DisplayIcon", "/d", ico)
	reg(un, "/v", "Publisher", "/d", "Texora")
	reg(un, "/v", "DisplayVersion", "/d", version)
	reg(un, "/v", "UninstallString", "/d", `"`+exe+`" --uninstall`)
	reg(un, "/v", "InstallLocation", "/d", dir)
	reg(un, "/v", "NoModify", "/t", "REG_DWORD", "/d", "1")
	reg(un, "/v", "NoRepair", "/t", "REG_DWORD", "/d", "1")
	// сообщаем Проводнику, что сопоставления файлов изменились
	_ = runPowerShell(`Add-Type -Namespace W -Name S -MemberDefinition '[DllImport("shell32.dll")] public static extern void SHChangeNotify(int e, int f, System.IntPtr a, System.IntPtr b);'; [W.S]::SHChangeNotify(0x08000000, 0, [System.IntPtr]::Zero, [System.IntPtr]::Zero)`)
	return exe, nil
}

func uninstall() {
	if messageBox(appName, "Удалить Texora с этого компьютера?\n\nВаши документы не пострадают.", mbYesNo|mbIconQ) != idYes {
		return
	}
	_ = runPowerShell(`foreach ($dir in @([Environment]::GetFolderPath('Desktop'), [Environment]::GetFolderPath('Programs'))) {
  Remove-Item -LiteralPath (Join-Path $dir 'Texora.lnk') -ErrorAction SilentlyContinue
  Remove-Item -LiteralPath (Join-Path $dir 'Удалить Texora.lnk') -ErrorAction SilentlyContinue }`)
	for _, k := range []string{`HKCU\Software\Classes\Texora.Document`, `HKCU\Software\Classes\Applications\Texora.exe`,
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\Texora`} {
		_ = hidden(exec.Command("reg", "delete", k, "/f")).Run()
	}
	for _, ext := range []string{".docx", ".doc", ".odt", ".rtf", ".txt", ".pdf", ".pptx"} {
		_ = hidden(exec.Command("reg", "delete", `HKCU\Software\Classes\`+ext+`\OpenWithProgids`, "/v", "Texora.Document", "/f")).Run()
	}
	_ = os.RemoveAll(dataDir())
	// сам exe удалить нельзя, пока он запущен — удаляем папку чуть позже
	_ = hidden(exec.Command("cmd", "/c", "ping 127.0.0.1 -n 3 >nul & rmdir /s /q \""+installDir()+"\"")).Start()
	showInfo(appName, "Texora удалён.")
}

func platformPreRun(args []string) bool {
	has := func(f string) bool {
		for _, a := range args {
			if a == f {
				return true
			}
		}
		return false
	}
	if has("--uninstall") {
		uninstall()
		return true
	}
	self, _ := os.Executable()
	inInstall := strings.HasPrefix(strings.ToLower(filepath.Clean(self)), strings.ToLower(filepath.Clean(installDir())))
	if inInstall || has("--portable") || has("--no-window") {
		return false
	}
	marker := filepath.Join(dataDir(), "portable")
	if !has("--install") {
		if _, err := os.Stat(marker); err == nil {
			return false
		}
	}
	ans := idYes
	if !has("--install") {
		ans = messageBox(appName, "Установить Texora на этот компьютер?\n\n"+
			"«Да» — установить: ярлыки на рабочем столе и в меню «Пуск», документы можно открывать через «Открыть с помощью».\n"+
			"«Нет» — просто запустить, ничего не устанавливая.\n\nИнтернет не нужен.", mbYesNoCancel|mbIconQ)
	}
	switch ans {
	case idYes:
		exe, err := install()
		if err != nil {
			showInfo(appName, "Не удалось установить: "+err.Error()+"\n\nПрограмма запустится без установки.")
			return false
		}
		rest := []string{}
		for _, a := range args {
			if a != "--install" {
				rest = append(rest, a)
			}
		}
		if exec.Command(exe, rest...).Start() == nil {
			showInfo(appName, "Texora установлен. Ярлык — на рабочем столе.")
			return true
		}
		return false
	case idNo:
		_ = os.MkdirAll(dataDir(), 0o755)
		_ = os.WriteFile(marker, []byte("1"), 0o644)
		return false
	default:
		return true
	}
}

// removeLegacy — убирает следы прежнего имени программы (WordLike): ярлыки, сопоставления, папку.
func removeLegacy() {
	old := filepath.Join(localAppData(), "Programs", "WordLike")
	if _, err := os.Stat(old); err != nil {
		return
	}
	_ = runPowerShell(`foreach ($dir in @([Environment]::GetFolderPath('Desktop'), [Environment]::GetFolderPath('Programs'))) {
  Remove-Item -LiteralPath (Join-Path $dir 'WordLike.lnk') -ErrorAction SilentlyContinue
  Remove-Item -LiteralPath (Join-Path $dir 'Удалить WordLike.lnk') -ErrorAction SilentlyContinue }`)
	for _, k := range []string{`HKCU\Software\Classes\WordLike.Document`, `HKCU\Software\Classes\Applications\WordLike.exe`,
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\WordLike`} {
		_ = hidden(exec.Command("reg", "delete", k, "/f")).Run()
	}
	for _, ext := range []string{".docx", ".doc", ".odt", ".rtf", ".txt", ".pdf"} {
		_ = hidden(exec.Command("reg", "delete", `HKCU\Software\Classes\`+ext+`\OpenWithProgids`, "/v", "WordLike.Document", "/f")).Run()
	}
	_ = os.RemoveAll(old)
	_ = os.RemoveAll(filepath.Join(localAppData(), "WordLike"))
}
