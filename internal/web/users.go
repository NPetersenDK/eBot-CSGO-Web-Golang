package web

import (
	"context"
	"errors"
	"net/mail"
	"net/url"
	"strings"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/auth"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

type userStore interface {
	ListUsers(ctx context.Context) ([]store.User, error)
	GetUser(ctx context.Context, id int64) (*store.User, error)
	CreateUser(ctx context.Context, in store.UserInput) (int64, error)
	UpdateUser(ctx context.Context, id int64, in store.UserInput) error
	DeleteUser(ctx context.Context, id int64) error
}

func isSelf(ctx context.Context, u *store.User) bool {
	s, ok := auth.FromContext(ctx)
	return ok && s.Source == auth.SourceLocal && strings.EqualFold(s.Subject, u.Username)
}

// otherSuperAdmins counts active super admins besides u.
func otherSuperAdmins(ctx context.Context, st userStore, u *store.User) (int, error) {
	users, err := st.ListUsers(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, o := range users {
		if o.ID != u.ID && o.IsSuperAdmin && o.IsActive {
			n++
		}
	}
	return n, nil
}

func userEntity(st userStore) *entity {
	fields := func(edit bool) []field {
		pwHelp := ""
		if edit {
			pwHelp = "Leave empty to keep the current password."
		}
		return []field{
			{Name: "first_name", Label: "First name", Type: "text", Max: 255},
			{Name: "last_name", Label: "Last name", Type: "text", Max: 255},
			{Name: "email", Label: "Email address", Type: "text", Required: true, Max: 255,
				Validate: func(v string, _ url.Values) string {
					if a, err := mail.ParseAddress(v); err != nil || a.Address != v {
						return "Enter a valid email address."
					}
					return ""
				}},
			{Name: "username", Label: "Username", Type: "text", Required: true, Max: 128},
			{Name: "password", Label: "Password", Type: "password", Required: !edit, Max: 128,
				Help: pwHelp},
			{Name: "password_again", Label: "Password again", Type: "password", Max: 128, ValidateEmpty: true,
				Validate: func(v string, f url.Values) string {
					if v != f.Get("password") {
						return "The two passwords must be the same."
					}
					return ""
				}},
			{Name: "is_active", Label: "Active", Type: "checkbox"},
			{Name: "is_super_admin", Label: "Super admin", Type: "checkbox", Help: "Super admins can log in and change everything."},
		}
	}
	input := func(v url.Values) store.UserInput {
		return store.UserInput{
			FirstName: v.Get("first_name"), LastName: v.Get("last_name"), Email: v.Get("email"),
			Username: v.Get("username"), Password: v.Get("password"),
			IsActive: v.Get("is_active") != "", IsSuperAdmin: v.Get("is_super_admin") != "",
		}
	}
	dupError := func(ctx context.Context, in store.UserInput, id int64) error {
		users, err := st.ListUsers(ctx)
		if err == nil {
			for _, u := range users {
				if u.ID == id {
					continue
				}
				if strings.EqualFold(u.Email, in.Email) {
					return fieldErrors{"email": "This email address is already in use."}
				}
			}
		}
		return fieldErrors{"username": "This username is already in use."}
	}
	return &entity{
		Slug:      "users",
		Singular:  "User",
		Plural:    "User management",
		NewLabel:  "Create new user",
		AdminOnly: true,
		Columns:   []string{"Username", "Email", "Role", "Created", "Updated", "Last login"},
		Empty:     "No users yet.",
		Defaults:  url.Values{"is_active": {"1"}},
		Fields:    func(_ context.Context, edit bool) ([]field, error) { return fields(edit), nil },
		Rows: func(ctx context.Context) ([]listRow, error) {
			list, err := st.ListUsers(ctx)
			if err != nil {
				return nil, err
			}
			rows := make([]listRow, 0, len(list))
			for i := range list {
				u := &list[i]
				role := listCell{Text: "Viewer", Badge: "muted"}
				if u.IsSuperAdmin {
					role = listCell{Text: "Super admin", Badge: "accent"}
				}
				if !u.IsActive {
					role = listCell{Text: "Inactive", Badge: "err"}
				}
				rows = append(rows, listRow{
					ID: u.ID,
					Cells: []listCell{
						{Text: u.Username}, {Text: u.Email}, role,
						{Text: u.CreatedAt.Format("2006-01-02 15:04")}, {Text: u.UpdatedAt.Format("2006-01-02 15:04")},
						{Text: lastLogin(u)},
					},
					CanDelete:  !isSelf(ctx, u),
					DeleteText: "Delete user " + u.Username + "?",
				})
			}
			return rows, nil
		},
		Load: func(ctx context.Context, id int64) (url.Values, error) {
			u, err := st.GetUser(ctx, id)
			if err != nil {
				return nil, err
			}
			return url.Values{
				"first_name": {u.FirstName}, "last_name": {u.LastName}, "email": {u.Email}, "username": {u.Username},
				"is_active": {boolText(u.IsActive)}, "is_super_admin": {boolText(u.IsSuperAdmin)},
			}, nil
		},
		Create: func(ctx context.Context, v url.Values) (string, error) {
			in := input(v)
			_, err := st.CreateUser(ctx, in)
			if errors.Is(err, store.ErrDuplicate) {
				return "", dupError(ctx, in, 0)
			}
			return "", err
		},
		Update: func(ctx context.Context, id int64, v url.Values) error {
			in := input(v)
			cur, err := st.GetUser(ctx, id)
			if err != nil {
				return err
			}
			if !in.IsActive || !in.IsSuperAdmin {
				if isSelf(ctx, cur) {
					return userError("You cannot deactivate yourself or remove your own super admin rights.")
				}
				if cur.IsSuperAdmin && cur.IsActive {
					n, err := otherSuperAdmins(ctx, st, cur)
					if err != nil {
						return err
					}
					if n == 0 {
						return userError("This is the last active super admin.")
					}
				}
			}
			err = st.UpdateUser(ctx, id, in)
			if errors.Is(err, store.ErrDuplicate) {
				return dupError(ctx, in, id)
			}
			return err
		},
		Delete: func(ctx context.Context, id int64) error {
			cur, err := st.GetUser(ctx, id)
			if err != nil {
				return err
			}
			if isSelf(ctx, cur) {
				return userError("You cannot delete your own account.")
			}
			if cur.IsSuperAdmin && cur.IsActive {
				n, err := otherSuperAdmins(ctx, st, cur)
				if err != nil {
					return err
				}
				if n == 0 {
					return userError("This is the last active super admin.")
				}
			}
			return st.DeleteUser(ctx, id)
		},
	}
}

func lastLogin(u *store.User) string {
	if u.LastLogin == nil {
		return ""
	}
	return u.LastLogin.Format("2006-01-02 15:04")
}
