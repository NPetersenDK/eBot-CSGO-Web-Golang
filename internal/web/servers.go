package web

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

type serverStore interface {
	ListServers(ctx context.Context) ([]store.Server, error)
	GetServer(ctx context.Context, id int64) (*store.Server, error)
	CreateServers(ctx context.Context, in store.ServerCreate) (int, error)
	UpdateServer(ctx context.Context, id int64, in store.ServerInput) error
	DeleteServer(ctx context.Context, id int64) error
}

const maxServerRange = 256

var hostnamePattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`)

func validPort(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1 && n <= 65535
}

func serverEntity(st serverStore) *entity {
	create := []field{
		{Name: "mode", Label: "Address type", Type: "select", Required: true, Options: []option{
			{"single", "Single server (IP and port)"},
			{"range", "Several ports on one IP"},
			{"hostname", "DNS hostname"},
		}, Help: "Servers added by DNS hostname cannot be controlled by the bot."},
		{Name: "host", Label: "Server IP or hostname", Type: "text", Required: true, Max: 100,
			Validate: func(v string, f url.Values) string {
				if f.Get("mode") == "hostname" {
					if !hostnamePattern.MatchString(v) {
						return "Enter a valid DNS name such as game.example.com."
					}
					return ""
				}
				if ip := net.ParseIP(v); ip == nil || ip.To4() == nil {
					return "Enter a valid IPv4 address."
				}
				return ""
			}},
		{Name: "port", Label: "Port", Type: "number", Required: true, Help: "First port in range mode.",
			Validate: func(v string, _ url.Values) string {
				if !validPort(v) {
					return "Port must be 1 to 65535."
				}
				return ""
			}},
		{Name: "port_end", Label: "Last port", Type: "number", ValidateEmpty: true, Help: "Range mode only. Creates one server per port from Port through Last port.",
			Validate: func(v string, f url.Values) string {
				if f.Get("mode") != "range" {
					return ""
				}
				if !validPort(v) {
					return "Port must be 1 to 65535."
				}
				first, _ := strconv.Atoi(f.Get("port"))
				last, _ := strconv.Atoi(v)
				if last < first {
					return "Must not be lower than the first port."
				}
				if last-first >= maxServerRange {
					return fmt.Sprintf("At most %d servers at once.", maxServerRange)
				}
				return ""
			}},
		{Name: "hostname", Label: "Internal server name", Type: "text", Required: true, Max: 90,
			Help: "A number is appended in range mode."},
		{Name: "rcon", Label: "RCON password", Type: "text", Required: true, Max: 50},
		{Name: "tv_ip", Label: "GOTV IP", Type: "text", Max: 100},
	}
	edit := []field{
		{Name: "ip", Label: "Server IP", Type: "text", Required: true, Max: 50, Help: "Host and port, for example 10.0.0.5:27015.",
			Validate: func(v string, _ url.Values) string {
				host, port, err := net.SplitHostPort(v)
				if err != nil || host == "" || !validPort(port) {
					return "Use the form host:port."
				}
				return ""
			}},
		{Name: "hostname", Label: "Internal server name", Type: "text", Required: true, Max: 100},
		{Name: "rcon", Label: "RCON password", Type: "password", Max: 50, Help: "Leave empty to keep the current password."},
		{Name: "tv_ip", Label: "GOTV IP", Type: "text", Max: 100},
	}

	return &entity{
		Slug:     "servers",
		Singular: "Server",
		Plural:   "Server management",
		NewLabel: "Create new server",
		Columns:  []string{"#ID", "IP address", "Name", "In use"},
		Empty:    "No servers registered.",
		Note:     "Servers added by DNS hostname cannot be controlled by the bot.",
		FormNote: "To add several game servers at once choose \"Several ports on one IP\". Servers added by DNS hostname cannot be controlled by the bot.",
		Defaults: url.Values{"mode": {"single"}},
		Fields: func(_ context.Context, isEdit bool) ([]field, error) {
			if isEdit {
				return edit, nil
			}
			return create, nil
		},
		Rows: func(ctx context.Context) ([]listRow, error) {
			servers, err := st.ListServers(ctx)
			if err != nil {
				return nil, err
			}
			rows := make([]listRow, 0, len(servers))
			for _, s := range servers {
				used := listCell{Text: "Not used", Badge: "muted"}
				if s.InUse {
					used = listCell{Text: "In use", Badge: "ok"}
				}
				rows = append(rows, listRow{
					ID:         s.ID,
					Cells:      []listCell{{Text: int64Text(s.ID)}, {Text: s.IP, Mono: true}, {Text: s.Hostname}, used},
					CanDelete:  !s.InUse,
					DeleteText: "Delete server " + s.IP + "?",
				})
			}
			return rows, nil
		},
		Load: func(ctx context.Context, id int64) (url.Values, error) {
			s, err := st.GetServer(ctx, id)
			if err != nil {
				return nil, err
			}
			return url.Values{"ip": {s.IP}, "hostname": {s.Hostname}, "tv_ip": {s.TVIP}}, nil
		},
		Create: func(ctx context.Context, v url.Values) (string, error) {
			in := store.ServerCreate{
				Mode:     v.Get("mode"),
				Host:     v.Get("host"),
				RCON:     v.Get("rcon"),
				Hostname: v.Get("hostname"),
				TVIP:     v.Get("tv_ip"),
			}
			in.Port, _ = strconv.Atoi(v.Get("port"))
			if in.Mode == "range" {
				in.PortEnd, _ = strconv.Atoi(v.Get("port_end"))
			}
			n, err := st.CreateServers(ctx, in)
			if err != nil {
				return "", err
			}
			if n == 1 {
				return "1 server added.", nil
			}
			return fmt.Sprintf("%d servers added.", n), nil
		},
		Update: func(ctx context.Context, id int64, v url.Values) error {
			in := store.ServerInput{IP: v.Get("ip"), RCON: v.Get("rcon"), Hostname: v.Get("hostname"), TVIP: v.Get("tv_ip")}
			if in.RCON == "" {
				cur, err := st.GetServer(ctx, id)
				if err != nil {
					return err
				}
				in.RCON = cur.RCON
			}
			return st.UpdateServer(ctx, id, in)
		},
		Delete: func(ctx context.Context, id int64) error {
			err := st.DeleteServer(ctx, id)
			if errors.Is(err, store.ErrServerBusy) {
				return userError("This server is currently in use.")
			}
			return err
		},
	}
}
