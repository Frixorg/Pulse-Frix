package httpx

import (
	"errors"
	"net/http"

	"github.com/frix-me/pulse/api/internal/auth"
	"github.com/frix-me/pulse/api/internal/model"
	"github.com/frix-me/pulse/api/internal/store"
)

// First-boot administrator provisioning.
//
// A deployment can be provisioned in two ways, and they are mutually exclusive
// in practice:
//
//  1. ADMIN_EMAIL + ADMIN_PASSWORD in the environment — the installer's path.
//     The owner is seeded at start-up (see cmd/pulse-api/main.go), so by the
//     time the API serves traffic an account already exists.
//  2. The interactive wizard at /setup, used when those variables are unset.
//
// The wizard is therefore gated on a single fact: the control plane holds no
// accounts at all. Once one exists the endpoint is closed permanently, so it
// can never be used to add a second owner or to overwrite the first.

type setupStatusResponse struct {
	NeedsSetup bool   `json:"needs_setup"`
	Mode       string `json:"mode"`
	SelfHosted bool   `json:"self_hosted"`
	MinPwdLen  int    `json:"min_password_length"`
}

type setupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// handleSetupStatus is public: it reveals only whether this deployment has been
// provisioned yet, which an unprovisioned login page has to know anyway.
func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, setupStatusResponse{
		NeedsSetup: s.needsSetup(),
		Mode:       s.cfg.Mode,
		SelfHosted: s.cfg.SelfHosted(),
		MinPwdLen:  auth.MinPasswordLength,
	})
}

// needsSetup reports whether the wizard should be offered. A store that cannot
// answer is treated as provisioned — failing closed keeps the endpoint shut
// rather than opening it on a transient database error.
func (s *Server) needsSetup() bool {
	if !s.cfg.EnableSetupWizard {
		return false
	}
	// Cloud is multi-tenant and provisions through an identity provider — the
	// first OIDC sign-in creates the tenant. A public account-creating endpoint
	// there would be attack surface with nothing to do.
	if !s.cfg.SelfHosted() {
		return false
	}
	n, err := s.store.CountUsers()
	if err != nil {
		s.logger.Warn("setup: could not count users", "error", err)
		return false
	}
	return n == 0
}

// handleSetupComplete creates the first owner account and signs it in.
func (s *Server) handleSetupComplete(w http.ResponseWriter, r *http.Request) {
	var req setupRequest
	if err := decodeJSON(r, &req, 4096); err != nil {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "invalid request body")
		return
	}

	// One provisioning attempt at a time, so two concurrent wizards cannot both
	// pass the "no accounts yet" check. (A multi-replica control plane is also
	// protected by the UNIQUE constraint on users.email.)
	s.setupMu.Lock()
	defer s.setupMu.Unlock()

	if !s.needsSetup() {
		Fail(w, r, http.StatusConflict, CodeValidation, "this deployment is already provisioned")
		return
	}

	email := auth.NormalizeEmail(req.Email)
	if err := auth.ValidateEmail(email); err != nil {
		Fail(w, r, http.StatusBadRequest, CodeValidation, err.Error())
		return
	}
	if err := auth.ValidatePasswordPolicy(req.Password, email); err != nil {
		Fail(w, r, http.StatusBadRequest, CodeValidation, err.Error())
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "password is not acceptable")
		return
	}
	org, user, err := s.store.SeedOrgOwner("Default", email, hash)
	if err != nil {
		if errors.Is(err, store.ErrAlreadyExists) {
			Fail(w, r, http.StatusConflict, CodeValidation, "this deployment is already provisioned")
			return
		}
		s.logger.Error("setup: could not seed owner", "error", err)
		Fail(w, r, http.StatusInternalServerError, CodeInternal, "could not create the administrator account")
		return
	}

	if err := s.issueSession(w, org.ID, user.ID); err != nil {
		// The account exists and is usable; only the auto-login failed.
		s.audit.Record(org.ID, email, "setup.complete", "success", clientIP(r), nil)
		Fail(w, r, http.StatusInternalServerError, CodeInternal, "account created, but the session could not be started — please sign in")
		return
	}
	s.audit.Record(org.ID, email, "setup.complete", "success", clientIP(r), nil)
	s.logger.Info("setup: administrator provisioned via wizard", "email", email)
	JSON(w, http.StatusCreated, sessionPayload(email, model.RoleOwner))
}
