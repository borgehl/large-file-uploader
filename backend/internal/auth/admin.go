package auth

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"

	"lfu/internal/httpx"
	"lfu/internal/store"
)

var usernameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{3,40}$`)

func (s *Service) AdminListUsers(w http.ResponseWriter, r *http.Request) {
	us, err := s.Store.ListUsers()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.JSON(w, http.StatusOK, us)
}

type createUserReq struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	Password string `json:"password"` // optional; generated when empty
}

func (s *Service) AdminCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserReq
	if !httpx.Decode(w, r, &req) {
		return
	}
	if !usernameRE.MatchString(req.Username) {
		httpx.Error(w, http.StatusBadRequest, "username must be 3-40 characters: letters, digits, . _ -")
		return
	}
	if req.Role == "" {
		req.Role = "user"
	}
	if req.Role != "user" && req.Role != "admin" {
		httpx.Error(w, http.StatusBadRequest, "role must be user or admin")
		return
	}
	pw := req.Password
	generated := false
	if pw == "" {
		var err error
		if pw, err = RandomPassword(); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "internal error")
			return
		}
		generated = true
	} else if err := ValidatePassword(pw); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	h, err := HashPassword(pw)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	u, err := s.Store.CreateUser(req.Username, h, req.Role, false, true)
	if errors.Is(err, store.ErrConflict) {
		httpx.Error(w, http.StatusConflict, "username already exists")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.Log.Info("user created", "user", u.Username, "role", u.Role, "by", UserFrom(r.Context()).Username)
	resp := map[string]any{"user": u}
	if generated {
		resp["temp_password"] = pw
	}
	httpx.JSON(w, http.StatusCreated, resp)
}

type patchUserReq struct {
	Disabled      *bool `json:"disabled"`
	ResetPassword bool  `json:"reset_password"`
}

func (s *Service) targetUser(w http.ResponseWriter, r *http.Request) *store.User {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return nil
	}
	u, err := s.Store.UserByID(id)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "user not found")
		return nil
	}
	return u
}

func (s *Service) AdminPatchUser(w http.ResponseWriter, r *http.Request) {
	var req patchUserReq
	if !httpx.Decode(w, r, &req) {
		return
	}
	t := s.targetUser(w, r)
	if t == nil {
		return
	}
	me := UserFrom(r.Context())
	if t.IsRoot || t.ID == me.ID {
		httpx.Error(w, http.StatusForbidden, "this account cannot be changed here")
		return
	}
	resp := map[string]any{}
	if req.Disabled != nil {
		if err := s.Store.SetDisabled(t.ID, *req.Disabled); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "internal error")
			return
		}
		if *req.Disabled {
			s.Store.DeleteUserSessions(t.ID, "")
		}
	}
	if req.ResetPassword {
		pw, err := RandomPassword()
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "internal error")
			return
		}
		h, err := HashPassword(pw)
		if err != nil || s.Store.SetPassword(t.ID, h, true) != nil {
			httpx.Error(w, http.StatusInternalServerError, "internal error")
			return
		}
		s.Store.DeleteUserSessions(t.ID, "")
		resp["temp_password"] = pw
	}
	s.Log.Info("user updated", "user", t.Username, "by", me.Username)
	resp["user"], _ = s.Store.UserByID(t.ID)
	httpx.JSON(w, http.StatusOK, resp)
}

func (s *Service) AdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	t := s.targetUser(w, r)
	if t == nil {
		return
	}
	me := UserFrom(r.Context())
	if t.IsRoot || t.ID == me.ID {
		httpx.Error(w, http.StatusForbidden, "this account cannot be deleted")
		return
	}
	if err := s.Store.DeleteUser(t.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.Log.Info("user deleted", "user", t.Username, "by", me.Username)
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
