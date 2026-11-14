//go:build !windows

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
)

// mkres — значок .ico, манифест и app.rc для ресурсов exe. После неё:
//
//	cd assets && llvm-windres -c 65001 --target=pe-x86-64 -i app.rc -O coff -o ../rsrc_windows_amd64.syso
func mkres() {
	sizes := []int{16, 20, 24, 32, 40, 48, 64, 128, 256}
	var pngs [][]byte
	for _, s := range sizes {
		px := AppIcon(s)
		img := image.NewNRGBA(image.Rect(0, 0, s, s))
		for i, p := range px {
			img.Set(i%s, i/s, color.NRGBA{uint8(p >> 16), uint8(p >> 8), uint8(p), uint8(p >> 24)})
		}
		var b bytes.Buffer
		_ = png.Encode(&b, img)
		pngs = append(pngs, b.Bytes())
	}
	var ico bytes.Buffer
	binary.Write(&ico, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	off := 6 + 16*len(sizes)
	for i, s := range sizes {
		d := byte(s)
		if s >= 256 {
			d = 0
		}
		ico.Write([]byte{d, d, 0, 0})
		binary.Write(&ico, binary.LittleEndian, [2]uint16{1, 32})
		binary.Write(&ico, binary.LittleEndian, [2]uint32{uint32(len(pngs[i])), uint32(off)})
		off += len(pngs[i])
	}
	for _, p := range pngs {
		ico.Write(p)
	}
	_ = os.MkdirAll("assets", 0o755)
	_ = os.WriteFile("assets/Texora.ico", ico.Bytes(), 0o644)
	manifest := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0" xmlns:asmv3="urn:schemas-microsoft-com:asm.v3">
  <assemblyIdentity type="win32" name="Texora" version="1.0.0.0" processorArchitecture="amd64"/>
  <description>Texora</description>
  <dependency><dependentAssembly>
    <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
  </dependentAssembly></dependency>
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3"><security><requestedPrivileges>
    <requestedExecutionLevel level="asInvoker" uiAccess="false"/>
  </requestedPrivileges></security></trustInfo>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1"><application>
    <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
    <supportedOS Id="{1f676c76-80e1-4239-95bb-83d0f6d0da78}"/>
    <supportedOS Id="{4a2f28e3-53b9-4441-ba9c-d69d4a4a6e38}"/>
    <supportedOS Id="{35138b9a-5d96-4fbd-8e2d-a2440225f93a}"/>
  </application></compatibility>
  <asmv3:application><asmv3:windowsSettings>
    <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/pm</dpiAware>
    <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2</dpiAwareness>
    <longPathAware xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">true</longPathAware>
  </asmv3:windowsSettings></asmv3:application>
</assembly>
`
	_ = os.WriteFile("assets/app.manifest", []byte(manifest), 0o644)
	rc := fmt.Sprintf(`1 ICON "Texora.ico"
1 24 "app.manifest"
1 VERSIONINFO
FILEVERSION %[2]s
PRODUCTVERSION %[2]s
FILEOS 0x40004
FILETYPE 0x1
BEGIN
  BLOCK "StringFileInfo"
  BEGIN
    BLOCK "041904B0"
    BEGIN
      VALUE "CompanyName", "Texora"
      VALUE "FileDescription", "Texora — текстовый редактор"
      VALUE "FileVersion", "%[1]s"
      VALUE "ProductName", "Texora"
      VALUE "ProductVersion", "%[1]s"
      VALUE "OriginalFilename", "Texora.exe"
      VALUE "LegalCopyright", "Texora"
    END
  END
  BLOCK "VarFileInfo"
  BEGIN
    VALUE "Translation", 0x419, 1200
  END
END
`, AppVersion, strings.ReplaceAll(AppVersion, ".", ",")+",0")
	_ = os.WriteFile("assets/app.rc", []byte(rc), 0o644)
	fmt.Println("assets готовы")
}
