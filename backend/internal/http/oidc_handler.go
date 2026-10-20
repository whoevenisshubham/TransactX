package http

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/common"
)

const oidcCookie = "__Host-transactx_oidc"

func (h *Handler) admitGoogle(w http.ResponseWriter, r *http.Request, action string) (*auth.GoogleLogin, func(), bool) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	var g *auth.GoogleLogin
	if h.auth != nil {
		g = h.auth.Google()
	}
	if g == nil {
		writeAPIError(w, r, common.NewAPIError("GOOGLE_DISABLED", "Google sign-in is unavailable", 503))
		return nil, nil, false
	}
	if (action == "exchange" && r.Header.Get("Origin") != g.FrontendOrigin()) || (action == "start" && r.Header.Get("Origin") != "" && r.Header.Get("Origin") != g.FrontendOrigin()) {
		writeAPIError(w, r, common.NewAPIError("ORIGIN_DENIED", "Sign-in origin is not allowed", 403))
		return nil, nil, false
	}
	if err := g.Admit(r.Context(), action); err != nil {
		w.Header().Set("Retry-After", "60")
		status := http.StatusServiceUnavailable
		if errors.Is(err, auth.ErrOIDCLimit) {
			status = http.StatusTooManyRequests
		}
		writeAPIError(w, r, common.NewAPIError("AUTH_UNAVAILABLE", "Sign-in is temporarily unavailable", status))
		return nil, nil, false
	}
	select {
	case h.oidcSlots <- struct{}{}:
		return g, func() { <-h.oidcSlots }, true
	default:
		w.Header().Set("Retry-After", "10")
		writeAPIError(w, r, common.NewAPIError("AUTH_BUSY", "A sign-in is being verified. Retry shortly", 429))
		return nil, nil, false
	}
}
func (h *Handler) googleStart(w http.ResponseWriter, r *http.Request) {
	g, done, ok := h.admitGoogle(w, r, "start")
	if !ok {
		return
	}
	defer done()
	query := r.URL.Query()
	if len(r.URL.RawQuery) > 512 || len(query) != 1 || len(query["app_challenge"]) != 1 {
		writeAPIError(w, r, common.NewAPIError("INVALID_REQUEST", "Sign-in challenge is invalid", 400))
		return
	}
	location, binding, err := g.Start(r.Context(), query.Get("app_challenge"))
	if err != nil {
		writeAPIError(w, r, common.NewAPIError("GOOGLE_UNAVAILABLE", "Google sign-in could not be started", 503))
		return
	}
	http.SetCookie(w, &http.Cookie{Name: oidcCookie, Value: binding, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	http.Redirect(w, r, location, http.StatusFound)
}
func (h *Handler) googleCallback(w http.ResponseWriter, r *http.Request) {
	g, done, ok := h.admitGoogle(w, r, "callback")
	if !ok {
		return
	}
	defer done()
	redirect := func(reason, code string) {
		query := url.Values{}
		if code != "" {
			query.Set("code", code)
		} else {
			query.Set("error", reason)
		}
		http.Redirect(w, r, g.FrontendOrigin()+"/auth/callback?"+query.Encode(), http.StatusSeeOther)
	}
	cookie, err := r.Cookie(oidcCookie)
	query := r.URL.Query()
	if err != nil || len(r.URL.RawQuery) > 8192 || len(query["state"]) != 1 || len(query["code"]) > 1 || len(query["error"]) > 1 {
		redirect("invalid_flow", "")
		return
	}
	flow, err := g.ConsumeFlow(r.Context(), query.Get("state"), cookie.Value)
	if err != nil {
		redirect("invalid_flow", "")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: oidcCookie, Value: "", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0)})
	if query.Get("error") != "" {
		redirect("denied", "")
		return
	}
	code, err := g.Complete(r.Context(), query.Get("code"), flow)
	if errors.Is(err, auth.ErrOIDCLimit) {
		redirect("account_limit", "")
		return
	}
	if err != nil {
		redirect("verification_failed", "")
		return
	}
	redirect("", code)
}
func (h *Handler) googleExchange(w http.ResponseWriter, r *http.Request) {
	g, done, ok := h.admitGoogle(w, r, "exchange")
	if !ok {
		return
	}
	defer done()
	var input struct {
		Code     string `json:"code"`
		Verifier string `json:"verifier"`
	}
	if r.Header.Get("Content-Type") != "application/json" || decodeJSON(w, r, &input) != nil {
		writeAPIError(w, r, common.NewAPIError("INVALID_REQUEST", "Sign-in exchange is invalid", 400))
		return
	}
	result, err := g.Exchange(r.Context(), input.Code, input.Verifier)
	if err != nil {
		writeAPIError(w, r, common.NewAPIError("INVALID_EXCHANGE", "Sign-in exchange could not be verified. Start again", 401))
		return
	}
	writeData(w, http.StatusOK, r, result)
}
