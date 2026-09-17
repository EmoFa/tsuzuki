//go:build windows

package player

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/sys/windows/registry"
)

func TestWindowsCandidates(t *testing.T) {
	env := map[string]string{
		"ProgramFiles": `C:\Program Files`,
		"LocalAppData": `C:\Users\u\AppData\Local`,
	}
	reg := func(root registry.Key, path, name string) string {
		switch {
		case root == registry.CURRENT_USER && name == "" && filepath.Base(path) == "mpv.exe":
			return `"D:\Apps\mpv\mpv.exe"`
		case root == registry.CURRENT_USER && path == "Environment":
			return `C:\Tools\mpv;"C:\Quoted Dir"`
		}
		return ""
	}
	got := windowsCandidates(func(k string) string { return env[k] }, reg, `C:\Users\u\Downloads`, `C:\Users\u`)

	want := []string{
		`C:\Users\u\Downloads\mpv.exe`,
		`D:\Apps\mpv\mpv.exe`,
		`C:\Tools\mpv\mpv.exe`,
		`C:\Quoted Dir\mpv.exe`,
		`C:\Program Files\mpv\mpv.exe`,
		`C:\Program Files\MPV Player\mpv.exe`,
		`C:\Users\u\AppData\Local\Programs\mpv\mpv.exe`,
		`C:\Users\u\AppData\Local\Microsoft\WinGet\Links\mpv.exe`,
		`C:\Users\u\scoop\apps\mpv\current\mpv.exe`,
		`C:\Users\u\scoop\shims\mpv.exe`,
		`C:\ProgramData\chocolatey\bin\mpv.exe`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("candidates:\n%q\nwant:\n%q", got, want)
	}
}

func TestRegistryString(t *testing.T) {
	const path = `Software\tsuzuki-test`
	k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { registry.DeleteKey(registry.CURRENT_USER, path) })
	k.SetStringValue("", `C:\plain`)
	k.SetExpandStringValue("Path", `%SystemRoot%\mpv`)
	k.Close()

	if got := registryString(registry.CURRENT_USER, path, ""); got != `C:\plain` {
		t.Errorf("default value = %q", got)
	}
	if got := registryString(registry.CURRENT_USER, path, "Path"); got != os.Getenv("SystemRoot")+`\mpv` {
		t.Errorf("expanded = %q", got)
	}
	if got := registryString(registry.CURRENT_USER, path+`\missing`, ""); got != "" {
		t.Errorf("missing = %q", got)
	}
}
