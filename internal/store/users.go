package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/auth"
	"github.com/go-sql-driver/mysql"
)

const userCols = `id, first_name, last_name, email_address, username, is_active, is_super_admin, last_login, created_at, updated_at`

func scanUser(r rowScanner) (User, error) {
	var u User
	var first, last sql.NullString
	var login, c, up entityTime
	if err := r.Scan(&u.ID, &first, &last, &u.Email, &u.Username, &u.IsActive, &u.IsSuperAdmin, &login, &c, &up); err != nil {
		return u, err
	}
	u.FirstName, u.LastName = first.String, last.String
	u.LastLogin, u.CreatedAt, u.UpdatedAt = login.ptr(), c.Time, up.Time
	return u, nil
}

func isDuplicate(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sf_guard_user`).Scan(&n)
	return n, err
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userCols+` FROM sf_guard_user ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) GetUser(ctx context.Context, id int64) (*User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM sf_guard_user WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) CreateUser(ctx context.Context, in UserInput) (int64, error) {
	if in.Password == "" {
		return 0, errors.New("password is required")
	}
	salt := NewSalt(in.Username)
	now := nowUTC()
	res, err := s.db.ExecContext(ctx, `INSERT INTO sf_guard_user (first_name, last_name, email_address, username, algorithm, salt, password, is_active, is_super_admin, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullStr(in.FirstName), nullStr(in.LastName), in.Email, in.Username, sfGuardAlgorithm, salt, HashPassword(salt, in.Password),
		in.IsActive, in.IsSuperAdmin, now, now)
	if isDuplicate(err) {
		return 0, ErrDuplicate
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateUser keeps the stored salt when it can. A user with another algorithm
// or no salt gets a fresh sha1 salt when the password changes.
func (s *Store) UpdateUser(ctx context.Context, id int64, in UserInput) error {
	var salt, algorithm sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT salt, algorithm FROM sf_guard_user WHERE id = ?`, id).Scan(&salt, &algorithm)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	q := `UPDATE sf_guard_user SET first_name = ?, last_name = ?, email_address = ?, username = ?, is_active = ?, is_super_admin = ?, updated_at = ?`
	args := []any{nullStr(in.FirstName), nullStr(in.LastName), in.Email, in.Username, in.IsActive, in.IsSuperAdmin, nowUTC()}
	if in.Password != "" {
		sl := salt.String
		if sl == "" || algorithm.String != sfGuardAlgorithm {
			sl = NewSalt(in.Username)
		}
		q += `, algorithm = ?, salt = ?, password = ?`
		args = append(args, sfGuardAlgorithm, sl, HashPassword(sl, in.Password))
	}
	_, err = s.db.ExecContext(ctx, q+` WHERE id = ?`, append(args, id)...)
	if isDuplicate(err) {
		return ErrDuplicate
	}
	return err
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	if err := s.existsID(ctx, "sf_guard_user", id); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM sf_guard_user_group WHERE user_id = ?`,
		`DELETE FROM sf_guard_user_permission WHERE user_id = ?`,
		`DELETE FROM sf_guard_remember_key WHERE user_id = ?`,
		`DELETE FROM sf_guard_forgot_password WHERE user_id = ?`,
		`DELETE FROM sf_guard_user WHERE id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const hasAdminPermission = `EXISTS (
  SELECT 1 FROM sf_guard_user_permission up JOIN sf_guard_permission p ON p.id = up.permission_id
  WHERE up.user_id = u.id AND p.name = 'admin')
OR EXISTS (
  SELECT 1 FROM sf_guard_user_group ug
  JOIN sf_guard_group_permission gp ON gp.group_id = ug.group_id
  JOIN sf_guard_permission p ON p.id = gp.permission_id
  WHERE ug.user_id = u.id AND p.name = 'admin')`

// dummySalt and dummyHash make the unknown-user path do the same work.
var (
	dummySalt = "00000000000000000000000000000000"
	dummyHash = HashPassword(dummySalt, "dummy")
)

// VerifyLocal implements auth.LocalUsers against sf_guard_user.
func (s *Store) VerifyLocal(ctx context.Context, username, password string) (auth.Session, error) {
	var (
		id                                 int64
		first, last, salt, algorithm, hash sql.NullString
		active, super, admin               bool
	)
	err := s.db.QueryRowContext(ctx, `SELECT u.id, u.first_name, u.last_name, u.algorithm, u.salt, u.password, u.is_active, u.is_super_admin, (`+hasAdminPermission+`) FROM sf_guard_user u WHERE u.username = ?`, username).
		Scan(&id, &first, &last, &algorithm, &salt, &hash, &active, &super, &admin)
	if errors.Is(err, sql.ErrNoRows) {
		CheckPassword(dummySalt, password, dummyHash)
		return auth.Session{}, auth.ErrInvalidCredentials
	}
	if err != nil {
		return auth.Session{}, err
	}
	ok := CheckPassword(salt.String, password, hash.String)
	if !ok || algorithm.String != sfGuardAlgorithm || !active || !(super || admin) {
		return auth.Session{}, auth.ErrInvalidCredentials
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE sf_guard_user SET last_login = ? WHERE id = ?`, nowUTC(), id); err != nil {
		return auth.Session{}, err
	}
	name := strings.TrimSpace(first.String + " " + last.String)
	if name == "" {
		name = username
	}
	return auth.Session{Subject: username, Name: name, Role: auth.RoleAdmin, Source: auth.SourceLocal}, nil
}

// IsAdmin implements auth.RoleChecker. A missing user is not an admin.
func (s *Store) IsAdmin(ctx context.Context, username string) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(u.is_active = 1 AND (u.is_super_admin = 1 OR `+hasAdminPermission+`), 0) FROM sf_guard_user u WHERE u.username = ?`, username).Scan(&ok)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return ok, err
}

func (s *Store) TouchLastLogin(ctx context.Context, username string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sf_guard_user SET last_login = ? WHERE username = ?`, nowUTC(), username)
	return err
}
