package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/EmoFa/anitui/internal/httpx"
)

var _ httpx.ClearanceStore = (*Store)(nil)

func (s *Store) LoadClearance(ctx context.Context, host string) (*httpx.Clearance, error) {
	var ua, cookies, remoteIP string
	var obtained int64
	err := s.DB.QueryRowContext(ctx,
		"SELECT user_agent, cookies, remote_ip, obtained_at FROM clearances WHERE host = ?", host,
	).Scan(&ua, &cookies, &remoteIP, &obtained)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cl := &httpx.Clearance{UserAgent: ua, RemoteIP: remoteIP, ObtainedAt: time.Unix(obtained, 0)}
	var cks []*http.Cookie
	if err := json.Unmarshal([]byte(cookies), &cks); err != nil {
		return nil, err
	}
	cl.Cookies = cks
	return cl, nil
}

func (s *Store) SaveClearance(ctx context.Context, host string, c *httpx.Clearance) error {
	cookies, err := json.Marshal(c.Cookies)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO clearances (host, user_agent, cookies, remote_ip, obtained_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (host) DO UPDATE SET user_agent = excluded.user_agent, cookies = excluded.cookies,
			remote_ip = excluded.remote_ip, obtained_at = excluded.obtained_at`,
		host, c.UserAgent, string(cookies), c.RemoteIP, c.ObtainedAt.Unix())
	return err
}

func (s *Store) DeleteClearance(ctx context.Context, host string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM clearances WHERE host = ?", host)
	return err
}
