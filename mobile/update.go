package mobile

import (
	"github.com/EmoFa/tsuzuki/internal/buildinfo"
	"github.com/EmoFa/tsuzuki/internal/update"
)

// appUpdate is what the app learns about a newer release.
type appUpdate struct {
	Current, Latest string
	Available       bool
	// Dev: this build isn't a release, so releases say nothing about it.
	Dev bool
	// Notify: the first time this release is reported.
	Notify bool
	// APK downloads the release for this phone; empty when the release has
	// no APK for it, and then Page is where to get it.
	APK  string `json:",omitempty"`
	Page string
}

// CheckUpdate looks for a newer release, asking GitHub at most once a day.
// abi is the phone's (Build.SUPPORTED_ABIS[0], e.g. "arm64-v8a"), to pick its
// APK; with firstNotice, Notify is set only the first time a release is seen.
func (a *App) CheckUpdate(abi string, firstNotice bool) (string, error) {
	u := appUpdate{Current: buildinfo.Version, Dev: !update.IsRelease(buildinfo.Version), Page: update.ReleasesURL}
	if !a.core.Config.General.CheckUpdates {
		return encode(u, nil)
	}
	client, err := a.core.HTTP(a.ctx)
	if err != nil {
		return "", err
	}
	st, err := a.core.Store(a.ctx)
	if err != nil {
		return "", err
	}
	r, err := (&update.Checker{Client: client, Cache: st, URL: a.updateURL}).Latest(a.ctx)
	if err != nil {
		return "", err
	}
	u.Latest, u.Page = r.Version, r.URL
	u.Available = !u.Dev && update.Newer(buildinfo.Version, r.Version)
	if asset, ok := r.Asset("_android_" + abi + ".apk"); ok {
		u.APK = asset.URL
	} else if asset, ok := r.Asset("_android_universal.apk"); ok {
		u.APK = asset.URL
	}
	if u.Available && firstNotice {
		u.Notify = a.core.FirstNotice(a.ctx, u.Latest)
	}
	return encode(u, nil)
}
