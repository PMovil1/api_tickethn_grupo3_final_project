package response

import (
	"encoding/json"
	"net/http"
)

func JSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	// All callers use concrete response types. A write error means the client cannot receive the response.
	_ = json.NewEncoder(w).Encode(body)
}

func Error(w http.ResponseWriter, status int, code, message string) {
	type detail struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	JSON(w, status, struct {
		Error detail `json:"error"`
	}{Error: detail{Code: code, Message: message}})
}

func Unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "Autenticación inválida")
}
