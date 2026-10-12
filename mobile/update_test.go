package mobile

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/EmoFa/tsuzuki/internal/buildinfo"
)

func TestCheckUpdatePicksThisPhonesAPK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v0.9.0","html_url":"https://github.com/EmoFa/tsuzuki/releases/tag/v0.9.0","assets":[
			{"name":"tsuzuki_0.9.0_android_arm64-v8a.apk","browser_download_url":"https://x/arm64.apk"},
			{"name":"tsuzuki_0.9.0_android_universal.apk","browser_download_url":"https://x/universal.apk"}]}`))
	}))
	defer srv.Close()
	was := buildinfo.Version
	buildinfo.Version = "0.5.0"
	defer func() { buildinfo.Version = was }()

	a, err := NewApp(t.TempDir(), t.TempDir(), fakeHost{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.updateURL = srv.URL

	check := func(abi string) (u struct {
		Available, Notify bool
		Latest, APK       string
	}) {
		t.Helper()
		out, err := a.CheckUpdate(abi, true)
		if err != nil {
			t.Fatal(err)
		}
		json.Unmarshal([]byte(out), &u)
		return u
	}
	u := check("arm64-v8a")
	if !u.Available || u.Latest != "0.9.0" || u.APK != "https://x/arm64.apk" || !u.Notify {
		t.Errorf("arm64: %+v", u)
	}
	// Told once; and a phone without its own APK gets the universal one.
	u = check("x86_64")
	if u.Notify || u.APK != "https://x/universal.apk" {
		t.Errorf("x86_64: %+v", u)
	}
}
