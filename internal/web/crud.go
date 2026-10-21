package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/auth"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

const maxFormBytes = 1 << 20

type option struct {
	Value string
	Label string
}

// field describes one form input. Type is text, number, password, select,
// checkbox, textarea or multiselect.
type field struct {
	Name     string
	Label    string
	Type     string
	Required bool
	Max      int
	Options  []option
	Help     string
	// Validate runs after the generic checks on a non-empty value. It returns
	// an error message or "".
	Validate func(value string, form url.Values) string
	// ValidateEmpty runs Validate for an empty optional value too.
	ValidateEmpty bool
}

type optionView struct {
	Value    string
	Label    string
	Selected bool
}

type fieldView struct {
	field
	Value   string
	Checked bool
	Choices []optionView
	Error   string
}

type listCell struct {
	Text  string
	Link  string
	Badge string
	Mono  bool
}

type listRow struct {
	ID            int64
	Cells         []listCell
	Active        bool
	CanDeactivate bool
	CanDelete     bool
	DeleteText    string
}

type listPage struct {
	Heading  string
	Slug     string
	NewLabel string
	Columns  []string
	Rows     []listRow
	Empty    string
	Note     string
	Entity   string
	// Deactivate is true when the entity has a deactivate action.
	Deactivate bool
}

func (p listPage) Title() string { return p.Heading }

type formPage struct {
	Heading    string
	Action     string
	Back       string
	Submit     string
	Note       string
	Fields     []fieldView
	Error      string
	DeleteURL  string
	DeleteText string
}

func (p formPage) Title() string { return p.Heading }

// userError is shown to the user as a message. fieldErrors are shown next to
// the named fields. Anything else is a server error.
type userError string

func (e userError) Error() string { return string(e) }

type fieldErrors map[string]string

func (e fieldErrors) Error() string { return "invalid fields" }

// entity ties one admin resource to the generic list and form pages.
type entity struct {
	Slug      string
	Singular  string
	Plural    string
	NewLabel  string
	AdminOnly bool
	Columns   []string
	Note      string
	FormNote  string
	Empty     string

	Fields   func(ctx context.Context, edit bool) ([]field, error)
	Defaults url.Values
	Rows     func(ctx context.Context) ([]listRow, error)
	Load     func(ctx context.Context, id int64) (url.Values, error)
	// Create returns the flash message, or "" for the default one.
	Create     func(ctx context.Context, v url.Values) (string, error)
	Update     func(ctx context.Context, id int64, v url.Values) error
	Delete     func(ctx context.Context, id int64) error
	Deactivate func(ctx context.Context, id int64) error
}

func (e *entity) base() string { return "/admin/" + e.Slug }

func (e *entity) deleteText(name string) string {
	if name == "" {
		return fmt.Sprintf("Delete this %s?", strings.ToLower(e.Singular))
	}
	return fmt.Sprintf("Delete %s?", name)
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func (a *App) mountCrud(mux *http.ServeMux, st crudStore) {
	mux.Handle("GET /admin", auth.RequireUser(http.HandlerFunc(a.dashboard(st))))

	ents := []*entity{serverEntity(st), teamEntity(st), seasonEntity(st), adEntity(st), userEntity(st)}
	for _, e := range ents {
		read := auth.RequireUser
		if e.AdminOnly {
			read = auth.RequireAdmin
		}
		write := auth.RequireAdmin
		b := e.base()
		mux.Handle("GET "+b, read(http.HandlerFunc(a.crudList(e))))
		mux.Handle("GET "+b+"/new", read(http.HandlerFunc(a.crudNew(e))))
		mux.Handle("POST "+b, write(http.HandlerFunc(a.crudCreate(e))))
		mux.Handle("GET "+b+"/{id}/edit", read(http.HandlerFunc(a.crudEdit(e))))
		mux.Handle("POST "+b+"/{id}", write(http.HandlerFunc(a.crudUpdate(e))))
		mux.Handle("POST "+b+"/{id}/delete", write(http.HandlerFunc(a.crudDelete(e))))
		if e.Deactivate != nil {
			mux.Handle("POST "+b+"/{id}/deactivate", write(http.HandlerFunc(a.crudDeactivate(e))))
		}
	}
}

func (a *App) registerCrudRoutes(mux *http.ServeMux) { a.mountCrud(mux, a.Store) }

type dashboardPage struct {
	Counts store.Counts
}

func (dashboardPage) Title() string { return "Dashboard" }

func (a *App) dashboard(st crudStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := st.Counts(r.Context())
		if err != nil {
			a.ServerError(w, r, err)
			return
		}
		a.Render(w, r, "dashboard", dashboardPage{Counts: c})
	}
}

func (a *App) crudList(e *entity) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := e.Rows(r.Context())
		if err != nil {
			a.ServerError(w, r, err)
			return
		}
		a.Render(w, r, "list", listPage{
			Heading:    e.Plural,
			Slug:       e.Slug,
			NewLabel:   e.NewLabel,
			Columns:    e.Columns,
			Rows:       rows,
			Empty:      e.Empty,
			Note:       e.Note,
			Entity:     e.Singular,
			Deactivate: e.Deactivate != nil,
		})
	}
}

func (a *App) formView(ctx context.Context, e *entity, edit bool, vals url.Values, errs map[string]string) ([]fieldView, error) {
	fields, err := e.Fields(ctx, edit)
	if err != nil {
		return nil, err
	}
	views := make([]fieldView, 0, len(fields))
	for _, f := range fields {
		v := fieldView{field: f, Error: errs[f.Name]}
		switch f.Type {
		case "password":
		case "checkbox":
			v.Checked = vals.Get(f.Name) != ""
		case "multiselect":
			sel := map[string]bool{}
			for _, s := range vals[f.Name] {
				sel[s] = true
			}
			for _, o := range f.Options {
				v.Choices = append(v.Choices, optionView{o.Value, o.Label, sel[o.Value]})
			}
		case "select":
			v.Value = vals.Get(f.Name)
			for _, o := range f.Options {
				v.Choices = append(v.Choices, optionView{o.Value, o.Label, o.Value == v.Value})
			}
		default:
			v.Value = vals.Get(f.Name)
		}
		views = append(views, v)
	}
	return views, nil
}

func (a *App) renderForm(w http.ResponseWriter, r *http.Request, status int, e *entity, id int64, vals url.Values, errs map[string]string, formErr string) {
	edit := id != 0
	views, err := a.formView(r.Context(), e, edit, vals, errs)
	if err != nil {
		a.ServerError(w, r, err)
		return
	}
	p := formPage{
		Heading: "New " + strings.ToLower(e.Singular),
		Action:  e.base(),
		Back:    e.base(),
		Submit:  "Create",
		Note:    e.FormNote,
		Fields:  views,
		Error:   formErr,
	}
	if edit {
		p.Heading = "Edit " + strings.ToLower(e.Singular)
		p.Action = fmt.Sprintf("%s/%d", e.base(), id)
		p.Submit = "Save"
		if e.Delete != nil {
			p.DeleteURL = p.Action + "/delete"
			p.DeleteText = e.deleteText("")
		}
	}
	a.RenderStatus(w, r, status, "form", p)
}

func (a *App) crudNew(e *entity) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.renderForm(w, r, http.StatusOK, e, 0, e.Defaults, nil, "")
	}
}

func (a *App) crudEdit(e *entity) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(r)
		if !ok {
			a.NotFound(w, r)
			return
		}
		vals, err := e.Load(r.Context(), id)
		if errors.Is(err, store.ErrNotFound) {
			a.NotFound(w, r)
			return
		}
		if err != nil {
			a.ServerError(w, r, err)
			return
		}
		a.renderForm(w, r, http.StatusOK, e, id, vals, nil, "")
	}
}

func (a *App) readForm(w http.ResponseWriter, r *http.Request) (url.Values, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return nil, false
	}
	return r.PostForm, true
}

func (a *App) crudCreate(e *entity) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vals, ok := a.readForm(w, r)
		if !ok {
			return
		}
		a.submit(w, r, e, 0, vals)
	}
}

func (a *App) crudUpdate(e *entity) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(r)
		if !ok {
			a.NotFound(w, r)
			return
		}
		vals, ok := a.readForm(w, r)
		if !ok {
			return
		}
		a.submit(w, r, e, id, vals)
	}
}

func (a *App) submit(w http.ResponseWriter, r *http.Request, e *entity, id int64, vals url.Values) {
	ctx := r.Context()
	edit := id != 0
	fields, err := e.Fields(ctx, edit)
	if err != nil {
		a.ServerError(w, r, err)
		return
	}
	vals = cleanValues(fields, vals)
	errs := validateFields(fields, vals)
	var formErr string
	msg := e.Singular + " saved."
	if len(errs) == 0 {
		if edit {
			err = e.Update(ctx, id, vals)
		} else {
			var m string
			m, err = e.Create(ctx, vals)
			if m != "" {
				msg = m
			} else {
				msg = e.Singular + " created."
			}
		}
		var fe fieldErrors
		var ue userError
		switch {
		case err == nil:
			a.SetFlash(w, "ok", msg)
			Redirect(w, r, e.base())
			return
		case errors.Is(err, store.ErrNotFound):
			a.NotFound(w, r)
			return
		case errors.As(err, &fe):
			errs = fe
		case errors.As(err, &ue):
			formErr = string(ue)
		case errors.Is(err, store.ErrInvalid):
			formErr = err.Error()
		default:
			a.ServerError(w, r, err)
			return
		}
	}
	a.renderForm(w, r, http.StatusUnprocessableEntity, e, id, vals, errs, formErr)
}

// cleanValues keeps only the declared fields and trims plain text.
func cleanValues(fields []field, in url.Values) url.Values {
	out := url.Values{}
	for _, f := range fields {
		switch f.Type {
		case "multiselect":
			out[f.Name] = in[f.Name]
		case "password":
			out.Set(f.Name, in.Get(f.Name))
		case "checkbox":
			if in.Get(f.Name) != "" {
				out.Set(f.Name, "1")
			}
		default:
			out.Set(f.Name, strings.TrimSpace(in.Get(f.Name)))
		}
	}
	return out
}

func validateFields(fields []field, vals url.Values) map[string]string {
	errs := map[string]string{}
	for _, f := range fields {
		if msg := validateField(f, vals); msg != "" {
			errs[f.Name] = msg
		}
	}
	return errs
}

func validateField(f field, vals url.Values) string {
	if f.Type == "checkbox" {
		return ""
	}
	if f.Type == "multiselect" {
		allowed := map[string]bool{}
		for _, o := range f.Options {
			allowed[o.Value] = true
		}
		for _, v := range vals[f.Name] {
			if !allowed[v] {
				return "Invalid choice."
			}
		}
		if f.Required && len(vals[f.Name]) == 0 {
			return "Required."
		}
		return ""
	}
	v := vals.Get(f.Name)
	if v == "" {
		if f.Required {
			return "Required."
		}
		if f.ValidateEmpty && f.Validate != nil {
			return f.Validate("", vals)
		}
		return ""
	}
	if f.Max > 0 && utf8.RuneCountInString(v) > f.Max {
		return fmt.Sprintf("At most %d characters.", f.Max)
	}
	switch f.Type {
	case "number":
		if _, err := strconv.ParseInt(v, 10, 64); err != nil {
			return "Must be a whole number."
		}
	case "select":
		ok := false
		for _, o := range f.Options {
			if o.Value == v {
				ok = true
				break
			}
		}
		if !ok {
			return "Invalid choice."
		}
	}
	if f.Validate != nil {
		return f.Validate(v, vals)
	}
	return ""
}

func (a *App) crudDelete(e *entity) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(r)
		if !ok {
			a.NotFound(w, r)
			return
		}
		a.act(w, r, e, e.Delete(r.Context(), id), e.Singular+" deleted.")
	}
}

func (a *App) crudDeactivate(e *entity) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(r)
		if !ok {
			a.NotFound(w, r)
			return
		}
		a.act(w, r, e, e.Deactivate(r.Context(), id), e.Singular+" deactivated.")
	}
}

func (a *App) act(w http.ResponseWriter, r *http.Request, e *entity, err error, okMsg string) {
	var ue userError
	switch {
	case err == nil:
		a.SetFlash(w, "ok", okMsg)
	case errors.Is(err, store.ErrNotFound):
		a.NotFound(w, r)
		return
	case errors.As(err, &ue):
		a.SetFlash(w, "err", string(ue))
	case errors.Is(err, store.ErrInvalid):
		a.SetFlash(w, "err", err.Error())
	default:
		a.ServerError(w, r, err)
		return
	}
	Redirect(w, r, e.base())
}

func boolText(v bool) string {
	if v {
		return "1"
	}
	return ""
}

func activeBadge(active bool) listCell {
	if active {
		return listCell{Text: "Active", Badge: "ok"}
	}
	return listCell{Text: "Inactive", Badge: "muted"}
}

func int64Text(v int64) string { return strconv.FormatInt(v, 10) }

type crudStore interface {
	serverStore
	teamStore
	seasonStore
	adStore
	userStore
	Counts(ctx context.Context) (store.Counts, error)
}
