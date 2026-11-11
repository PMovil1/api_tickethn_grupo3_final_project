package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/auth"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/middleware"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/response"
	"github.com/go-chi/chi/v5"
)

type Auth struct {
	service *auth.Service
	logger  *slog.Logger
	limits  authLimiter
}

func NewAuth(service *auth.Service, logger *slog.Logger) *Auth {
	return &Auth{service: service, logger: logger, limits: authLimiter{counters: make(map[string]counter)}}
}

type registerInput struct {
	FullName string `json:"full_name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type codeInput struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type resetInput struct {
	Email       string `json:"email"`
	Code        string `json:"code"`
	NewPassword string `json:"new_password"`
}

type emailInput struct {
	Email string `json:"email"`
}

type refreshInput struct {
	RefreshToken string `json:"refresh_token"`
}

type userSummary struct {
	ID       string        `json:"id"`
	FullName string        `json:"full_name"`
	Email    string        `json:"email"`
	Roles    []domain.Role `json:"roles"`
}

type userProfile struct {
	userSummary
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
}

type tokenResponse struct {
	AccessToken  string      `json:"access_token"`
	RefreshToken string      `json:"refresh_token"`
	TokenType    string      `json:"token_type"`
	ExpiresIn    int64       `json:"expires_in"`
	User         userSummary `json:"user"`
}

type sessionResponse struct {
	ID         string     `json:"id"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	Status     string     `json:"status"`
	IP         string     `json:"ip_address,omitempty"`
	UserAgent  string     `json:"user_agent,omitempty"`
}

func (h *Auth) Register(w http.ResponseWriter, r *http.Request) {
	var body registerInput
	if !decodeAuth(w, r, &body) || !h.allow(w, r, "register", body.Email) {
		return
	}
	if err := h.service.Register(r.Context(), body.FullName, body.Email, body.Password); err != nil {
		h.fail(w, r, err)
		return
	}
	message(w, http.StatusCreated, "Cuenta creada. Revisa tu correo para verificarla")
}

func (h *Auth) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var body codeInput
	if !decodeAuth(w, r, &body) || !h.allow(w, r, "verify-email", body.Email) {
		return
	}
	if err := h.service.VerifyEmail(r.Context(), body.Email, body.Code); err != nil {
		h.fail(w, r, err)
		return
	}
	message(w, http.StatusOK, "Correo verificado")
}

func (h *Auth) ResendVerification(w http.ResponseWriter, r *http.Request) {
	h.requestCode(w, r, "resend-verification", domain.VerifyEmail)
}

func (h *Auth) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	h.requestCode(w, r, "forgot-password", domain.ResetPassword)
}

func (h *Auth) requestCode(w http.ResponseWriter, r *http.Request, action string, purpose domain.CodePurpose) {
	var body emailInput
	if !decodeAuth(w, r, &body) || !h.allow(w, r, action, body.Email) {
		return
	}
	if err := h.service.RequestCode(r.Context(), body.Email, purpose); err != nil {
		h.fail(w, r, err)
		return
	}
	message(w, http.StatusOK, "Si la cuenta cumple los requisitos, recibirás un correo con instrucciones")
}

func (h *Auth) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var body resetInput
	if !decodeAuth(w, r, &body) || !h.allow(w, r, "reset-password", body.Email) {
		return
	}
	if err := h.service.ResetPassword(r.Context(), body.Email, body.Code, body.NewPassword); err != nil {
		h.fail(w, r, err)
		return
	}
	message(w, http.StatusOK, "Contraseña restablecida")
}

func (h *Auth) Login(w http.ResponseWriter, r *http.Request) {
	var body loginInput
	if !decodeAuth(w, r, &body) || !h.allow(w, r, "login", body.Email) {
		return
	}
	pair, err := h.service.Login(r.Context(), body.Email, body.Password, requestClient(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writePair(w, pair)
}

func (h *Auth) Refresh(w http.ResponseWriter, r *http.Request) {
	var body refreshInput
	if !decodeAuth(w, r, &body) {
		return
	}
	pair, err := h.service.Refresh(r.Context(), body.RefreshToken, requestClient(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writePair(w, pair)
}

func (h *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	var body refreshInput
	if !decodeAuth(w, r, &body) {
		return
	}
	user, _ := middleware.Identity(r.Context())
	if err := h.service.Logout(r.Context(), user.ID, body.RefreshToken); err != nil {
		h.fail(w, r, err)
		return
	}
	message(w, http.StatusOK, "Sesión cerrada")
}

func (h *Auth) Me(w http.ResponseWriter, r *http.Request) {
	user, _ := middleware.Identity(r.Context())
	response.JSON(w, http.StatusOK, userProfile{
		userSummary: summary(user), EmailVerified: user.EmailVerifiedAt != nil, CreatedAt: user.CreatedAt.UTC(),
	})
}

func (h *Auth) Sessions(w http.ResponseWriter, r *http.Request) {
	user, _ := middleware.Identity(r.Context())
	sessions, err := h.service.Sessions(r.Context(), user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	result := make([]sessionResponse, 0, len(sessions))
	now := time.Now()
	for _, session := range sessions {
		status := "active"
		switch {
		case session.RevokedAt != nil:
			status = "revoked"
		case !session.ExpiresAt.After(now):
			status = "expired"
		}
		result = append(result, sessionResponse{
			ID: session.ID, CreatedAt: session.CreatedAt.UTC(), LastUsedAt: session.LastUsedAt,
			ExpiresAt: session.ExpiresAt.UTC(), Status: status, IP: session.Client.IP, UserAgent: session.Client.UserAgent,
		})
	}
	response.JSON(w, http.StatusOK, result)
}

func (h *Auth) RevokeSession(w http.ResponseWriter, r *http.Request) {
	user, _ := middleware.Identity(r.Context())
	if err := h.service.RevokeSession(r.Context(), user.ID, chi.URLParam(r, "id")); err != nil {
		h.fail(w, r, err)
		return
	}
	message(w, http.StatusOK, "Sesión revocada")
}

func decodeAuth(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil || decoder.Decode(new(any)) != io.EOF {
		response.Error(w, http.StatusBadRequest, "INVALID_INPUT", "Cuerpo JSON inválido")
		return false
	}
	return true
}

func requestClient(r *http.Request) domain.Client {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	var ip string
	if err == nil {
		if address, err := netip.ParseAddr(host); err == nil {
			ip = address.WithZone("").Unmap().String()
		}
	}
	userAgent := strings.ReplaceAll(r.UserAgent(), "\x00", "")
	if len(userAgent) > 512 {
		userAgent = userAgent[:512]
	}
	return domain.Client{IP: ip, UserAgent: strings.ToValidUTF8(userAgent, "")}
}

func (h *Auth) allow(w http.ResponseWriter, r *http.Request, action, address string) bool {
	email, _ := auth.NormalizeEmail(address)
	if retry := h.limits.allow(action, requestClient(r).IP, email, time.Now()); retry > 0 {
		tooManyRequests(w, retry)
		return false
	}
	return true
}

func (h *Auth) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "INVALID_INPUT", "Verifica los campos de la petición")
	case errors.Is(err, auth.ErrInvalidCode):
		response.Error(w, http.StatusBadRequest, "INVALID_CODE", "Código inválido o vencido")
	case errors.Is(err, auth.ErrDuplicate):
		response.Error(w, http.StatusConflict, "EMAIL_ALREADY_REGISTERED", "El correo ya está registrado")
	case errors.Is(err, auth.ErrCredentials):
		response.Error(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Credenciales inválidas")
	case errors.Is(err, auth.ErrUnauthorized):
		response.Unauthorized(w)
	case errors.Is(err, auth.ErrNotFound):
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "Sesión no encontrada")
	case errors.Is(err, context.DeadlineExceeded):
		h.logger.WarnContext(r.Context(), "Authentication request timed out")
		response.Error(w, http.StatusServiceUnavailable, "UNAVAILABLE", "Servicio temporalmente no disponible")
	default:
		h.logger.ErrorContext(r.Context(), "Authentication operation failed", "error_type", fmt.Sprintf("%T", err))
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Error interno del servidor")
	}
}

func summary(user domain.User) userSummary {
	roles := user.Roles
	if roles == nil {
		roles = []domain.Role{}
	}
	return userSummary{ID: user.ID, FullName: user.FullName, Email: user.Email, Roles: roles}
}

func writePair(w http.ResponseWriter, pair auth.TokenPair) {
	response.JSON(w, http.StatusOK, tokenResponse{
		AccessToken: pair.AccessToken, RefreshToken: pair.RefreshToken,
		TokenType: "Bearer", ExpiresIn: pair.ExpiresIn, User: summary(pair.User),
	})
}

func message(w http.ResponseWriter, status int, text string) {
	response.JSON(w, status, struct {
		Message string `json:"message"`
	}{Message: text})
}
