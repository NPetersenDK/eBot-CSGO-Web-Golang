package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// entityTime scans a DATETIME column whether or not the DSN has parseTime.
type entityTime struct{ time.Time }

func (t *entityTime) Scan(v any) error {
	switch x := v.(type) {
	case nil:
		t.Time = time.Time{}
	case time.Time:
		t.Time = x
	case []byte:
		return t.parse(string(x))
	case string:
		return t.parse(x)
	default:
		return fmt.Errorf("store: cannot scan %T into time", v)
	}
	return nil
}

func (t *entityTime) parse(s string) error {
	if s == "" || strings.HasPrefix(s, "0000-00-00") {
		t.Time = time.Time{}
		return nil
	}
	p, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.UTC)
	if err != nil {
		return err
	}
	t.Time = p
	return nil
}

func (t entityTime) ptr() *time.Time {
	if t.Time.IsZero() {
		return nil
	}
	v := t.Time
	return &v
}

func nowUTC() time.Time { return time.Now().UTC() }

// existsID reports whether a row with the id exists. table is always a constant.
func (s *Store) existsID(ctx context.Context, table string, id int64) error {
	var one int
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM "+table+" WHERE id = ?", id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

const serverInUse = `EXISTS (SELECT 1 FROM matchs m WHERE m.server_id = s.id AND m.enable = 1 AND m.status < 13)`

func (s *Store) queryServers(ctx context.Context, where string, args ...any) ([]Server, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.id, s.ip, s.rcon, s.hostname, s.tv_ip, s.created_at, s.updated_at, `+serverInUse+` FROM servers s `+where+` ORDER BY s.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Server
	for rows.Next() {
		var sv Server
		var tv sql.NullString
		var c, u entityTime
		if err := rows.Scan(&sv.ID, &sv.IP, &sv.RCON, &sv.Hostname, &tv, &c, &u, &sv.InUse); err != nil {
			return nil, err
		}
		sv.TVIP, sv.CreatedAt, sv.UpdatedAt = tv.String, c.Time, u.Time
		out = append(out, sv)
	}
	return out, rows.Err()
}

func (s *Store) ListServers(ctx context.Context) ([]Server, error) {
	return s.queryServers(ctx, "")
}

// FreeServers lists servers without an enabled match.
func (s *Store) FreeServers(ctx context.Context) ([]Server, error) {
	return s.queryServers(ctx, "WHERE NOT "+serverInUse)
}

func (s *Store) GetServer(ctx context.Context, id int64) (*Server, error) {
	list, err := s.queryServers(ctx, "WHERE s.id = ?", id)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return &list[0], nil
}

type newServer struct{ IP, Hostname string }

var (
	ipv4Re      = regexp.MustCompile(`^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$`)
	ipv4RangeRe = regexp.MustCompile(`^(\d{1,3}\.\d{1,3}\.\d{1,3}\.)(\d{1,3})-(\d{1,3})$`)
	dnsHostRe   = regexp.MustCompile(`^[a-zA-Z0-9.-]+\.[a-zA-Z]{2,6}$`)
)

// expandServers turns a ServerCreate into the servers to insert. Range mode
// walks the ports Port..PortEnd and, like the PHP action, also accepts a last
// octet range in Host ("10.0.0.1-5"). Numbered hostnames get " #n" appended.
func expandServers(in ServerCreate) ([]newServer, error) {
	host := strings.TrimSpace(in.Host)
	validPort := func(p int) bool { return p >= 1 && p <= 65535 }
	switch in.Mode {
	case "single":
		if !ipv4Re.MatchString(host) || !validPort(in.Port) {
			return nil, invalid("unknown server format")
		}
		return []newServer{{host + ":" + strconv.Itoa(in.Port), in.Hostname}}, nil
	case "hostname":
		if !dnsHostRe.MatchString(host) || !validPort(in.Port) {
			return nil, invalid("unknown server format")
		}
		return []newServer{{host + ":" + strconv.Itoa(in.Port), in.Hostname}}, nil
	case "range":
		if !validPort(in.Port) || in.PortEnd < in.Port || in.PortEnd > 65535 {
			return nil, invalid("invalid port range")
		}
		hosts := []string{host}
		if m := ipv4RangeRe.FindStringSubmatch(host); m != nil {
			start, _ := strconv.Atoi(m[2])
			end, _ := strconv.Atoi(m[3])
			if start > end || end > 255 {
				return nil, invalid("invalid address range")
			}
			hosts = hosts[:0]
			for i := start; i <= end; i++ {
				hosts = append(hosts, m[1]+strconv.Itoa(i))
			}
		} else if !ipv4Re.MatchString(host) {
			return nil, invalid("unknown server format")
		}
		var out []newServer
		for _, h := range hosts {
			for p := in.Port; p <= in.PortEnd; p++ {
				out = append(out, newServer{h + ":" + strconv.Itoa(p), fmt.Sprintf("%s #%d", in.Hostname, len(out)+1)})
			}
		}
		return out, nil
	}
	return nil, invalid("unknown server mode")
}

// CreateServers returns the number of servers created.
func (s *Store) CreateServers(ctx context.Context, in ServerCreate) (int, error) {
	list, err := expandServers(in)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := nowUTC()
	for _, sv := range list {
		_, err := tx.ExecContext(ctx, `INSERT INTO servers (ip, rcon, hostname, tv_ip, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			sv.IP, in.RCON, sv.Hostname, nullStr(in.TVIP), now, now)
		if err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(list), nil
}

func nullStr(v string) sql.NullString { return sql.NullString{String: v, Valid: v != ""} }

func (s *Store) UpdateServer(ctx context.Context, id int64, in ServerInput) error {
	res, err := s.db.ExecContext(ctx, `UPDATE servers SET ip = ?, rcon = ?, hostname = ?, tv_ip = ?, updated_at = ? WHERE id = ?`,
		in.IP, in.RCON, in.Hostname, nullStr(in.TVIP), nowUTC(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.existsID(ctx, "servers", id)
	}
	return nil
}

func (s *Store) DeleteServer(ctx context.Context, id int64) error {
	sv, err := s.GetServer(ctx, id)
	if err != nil {
		return err
	}
	if sv.InUse {
		return ErrServerBusy
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM servers WHERE id = ?`, id)
	return err
}
