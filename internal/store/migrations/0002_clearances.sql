-- Bot-protection clearances (cookies + the User-Agent they're bound to) per host.
CREATE TABLE clearances (
    host        TEXT PRIMARY KEY,
    user_agent  TEXT NOT NULL,
    cookies     TEXT NOT NULL, -- JSON array of http.Cookie
    remote_ip   TEXT NOT NULL DEFAULT '', -- server IP the browser used
    obtained_at INTEGER NOT NULL
);
