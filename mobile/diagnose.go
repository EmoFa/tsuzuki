package mobile

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/EmoFa/tsuzuki/internal/buildinfo"
	"github.com/EmoFa/tsuzuki/internal/core"
)

// diagnosis is a check as the app shows it.
type diagnosis struct {
	Name   string
	Status string // "ok", "warn" or "fail"
	Detail string
}

// Diagnose checks the app's setup, AniList, the skip-time and filler services,
// and a search on every configured provider; with streams it also resolves
// and checks an episode from each. It's the app's `tsuzuki doctor`, and takes
// up to a minute.
func (a *App) Diagnose(streams bool) (string, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 90*time.Second)
	defer cancel()
	checks := a.localChecks(ctx)
	checks = append(checks, core.RunProbes(ctx, 20*time.Second, a.core.ServiceProbes())...)
	checks = append(checks, a.core.ProviderChecks(ctx, "frieren", streams, 45*time.Second)...)
	return encode(diagnoses(checks), nil)
}

func diagnoses(checks []core.Check) []diagnosis {
	out := make([]diagnosis, len(checks))
	for i, c := range checks {
		out[i] = diagnosis{Name: c.Name, Status: [...]string{"ok", "warn", "fail"}[c.Status], Detail: c.Detail}
	}
	return out
}

// localChecksJSON is Diagnose without the network, for tests.
func (a *App) localChecksJSON() (string, error) {
	return encode(diagnoses(a.localChecks(a.ctx)), nil)
}

func (a *App) localChecks(ctx context.Context) []core.Check {
	checks := []core.Check{{Name: "tsuzuki", Status: core.CheckOK, Detail: fmt.Sprintf("%s, android/%s", buildinfo.Version, runtime.GOARCH)}}
	st, err := a.core.Store(ctx)
	if err != nil {
		return append(checks, core.Check{Name: "database", Status: core.CheckFail, Detail: err.Error()})
	}
	v, _ := st.SchemaVersion(ctx)
	checks = append(checks, core.Check{Name: "database", Status: core.CheckOK, Detail: fmt.Sprintf("schema v%d", v)})

	acc := a.core.Account()
	login := core.Check{Name: "anilist login", Status: core.CheckOK}
	switch {
	case acc.Backend == "local":
		login.Detail = "not used: your list stays on this phone"
	case acc.LoggedIn:
		login.Detail = "logged in as " + acc.User
	case acc.Expired:
		login.Status, login.Detail = core.CheckWarn, "login for "+acc.User+" expired: log in again in Settings"
	default:
		login.Status, login.Detail = core.CheckWarn, "not logged in: progress is kept on this phone until you log in"
	}
	checks = append(checks, login)

	if pending, err := st.PendingSyncs(ctx); err == nil && len(pending) > 0 {
		checks = append(checks, core.Check{Name: "sync queue", Status: core.CheckWarn, Detail: fmt.Sprintf(
			"%d list change(s) not sent to AniList yet; last error: %s", len(pending), core.FirstLine(pending[len(pending)-1].LastError))})
	}
	if list, err := st.Clearances(ctx); err == nil {
		detail := "none stored"
		if len(list) > 0 {
			var hosts []string
			for _, c := range list {
				hosts = append(hosts, fmt.Sprintf("%s (%s)", c.Host, c.ObtainedAt.Format("2006-01-02")))
			}
			slices.Sort(hosts)
			detail = strings.Join(hosts, ", ")
		}
		checks = append(checks, core.Check{Name: "site verifications", Status: core.CheckOK, Detail: detail})
	}
	return checks
}

// ClearVerifications forgets every site's stored bot-check clearance, for
// when a site keeps failing; the next visit verifies again.
func (a *App) ClearVerifications() error {
	st, err := a.core.Store(a.ctx)
	if err != nil {
		return err
	}
	list, err := st.Clearances(a.ctx)
	if err != nil {
		return err
	}
	for _, c := range list {
		if err := st.DeleteClearance(a.ctx, c.Host); err != nil {
			return err
		}
	}
	client, err := a.core.HTTP(a.ctx)
	if err != nil {
		return err
	}
	client.ForgetClearances()
	return nil
}
