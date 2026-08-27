package httpx

import (
	"errors"
	"net/http"

	"github.com/frix-me/pulse/api/internal/auth"
	"github.com/frix-me/pulse/api/internal/store"
)

// Self-service credential management for the signed-in account, backing the
// Security section of /app/settings.
//
// Both endpoints re-verify the CURRENT password before changing anything, so a
// hijacked session alone cannot lock the operator out of their own deployment.
// Every role may manage its own credentials — this is not an admin capability.

type changeEmailRequest struct {
	CurrentPassword string `json:"current_password"`
	Email           string `json:"email"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// verifyCurrentPassword loads the caller's user row and checks the supplied
// password against it. It returns a ready-to-send failure message on mismatch.
func (s *Server) verifyCurrentPassword(r *http.Request, current string) (bool, int, string) {
	p := s.principal(r)
	user, err := s.store.GetUser(p.OrgID, p.UserID)
	if err != nil {
		return false, http.StatusUnauthorized, "account not found"
	}
	if user.PasswordHash == "" {
		// Identity-provider accounts have no password to verify against, so
		// there is nothing this endpoint can safely authorise.
		return false, http.StatusConflict, "this account signs in with an identity provider and has no password"
	}
	if !auth.VerifyPassword(current, user.PasswordHash) {
		return false, http.StatusUnauthorized, "current password is incorrect"
	}
	return true, 0, ""
}

func (s *Server) handleChangeEmail(w http.ResponseWriter, r *http.Request) {
	var req changeEmailRequest
	if err := decodeJSON(r, &req, 4096); err != nil {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "invalid request body")
		return
	}
	p := s.principal(r)

	email := auth.NormalizeEmail(req.Email)
	if err := auth.ValidateEmail(email); err != nil {
		Fail(w, r, http.StatusBadRequest, CodeValidation, err.Error())
		return
	}
	if ok, status, msg := s.verifyCurrentPassword(r, req.CurrentPassword); !ok {
		s.audit.Record(p.OrgID, p.Email, "account.email_change", "failure", clientIP(r), nil)
		Fail(w, r, status, CodeAuth, msg)
		return
	}

	if err := s.store.UpdateUserEmail(p.OrgID, p.UserID, email); err != nil {
		if errors.Is(err, store.ErrAlreadyExists) {
			Fail(w, r, http.StatusConflict, CodeValidation, "that email is already in use")
			return
		}
		s.logger.Error("account: could not update email", "error", err)
		Fail(w, r, http.StatusInternalServerError, CodeInternal, "could not update the email address")
		return
	}
	s.audit.Record(p.OrgID, p.Email, "account.email_change", "success", clientIP(r),
		map[string]any{"new_email": email})
	JSON(w, http.StatusOK, sessionPayload(email, p.Role))
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req changePasswordRequest
	if err := decodeJSON(r, &req, 4096); err != nil {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "invalid request body")
		return
	}
	p := s.principal(r)

	if err := auth.ValidatePasswordPolicy(req.NewPassword, p.Email); err != nil {
		Fail(w, r, http.StatusBadRequest, CodeValidation, err.Error())
		return
	}
	if req.NewPassword == req.CurrentPassword {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "the new password must differ from the current one")
		return
	}
	if ok, status, msg := s.verifyCurrentPassword(r, req.CurrentPassword); !ok {
		s.audit.Record(p.OrgID, p.Email, "account.password_change", "failure", clientIP(r), nil)
		Fail(w, r, status, CodeAuth, msg)
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "password is not acceptable")
		return
	}
	if err := s.store.UpdateUserPassword(p.OrgID, p.UserID, hash); err != nil {
		s.logger.Error("account: could not update password", "error", err)
		Fail(w, r, http.StatusInternalServerError, CodeInternal, "could not update the password")
		return
	}

	// Session rotation. Every session the user holds — including this one — is
	// revoked, then a fresh cookie is issued to the browser that made the
	// change. A cookie stolen before the change stops working immediately.
	if err := s.store.DeleteSessionsForUser(p.UserID); err != nil {
		s.logger.Warn("account: could not revoke existing sessions", "error", err)
	}
	if err := s.issueSession(w, p.OrgID, p.UserID); err != nil {
		// The password did change; the browser simply has to sign in again.
		s.audit.Record(p.OrgID, p.Email, "account.password_change", "success", clientIP(r), nil)
		clearSessionCookie(w)
		Fail(w, r, http.StatusUnauthorized, CodeAuth, "password updated — please sign in again")
		return
	}
	s.audit.Record(p.OrgID, p.Email, "account.password_change", "success", clientIP(r), nil)
	JSON(w, http.StatusOK, sessionPayload(p.Email, p.Role))
}
