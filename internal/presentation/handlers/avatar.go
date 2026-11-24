package handlers

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/auth"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/middleware"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/response"
)

func (h *Auth) ReadAvatar(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	user, _ := middleware.Identity(r.Context())
	avatar, err := h.avatars.Read(r.Context(), user.ID)
	if err != nil {
		h.avatarError(w, r, err)
		return
	}
	if avatar == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", avatar.MediaType)
	w.Header().Set("Content-Length", strconv.Itoa(len(avatar.Bytes)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(avatar.Bytes)
}

func (h *Auth) ReplaceAvatar(w http.ResponseWriter, r *http.Request) {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch mediaType {
	case "image/jpeg", "image/png", "image/webp":
	default:
		response.Error(w, http.StatusUnsupportedMediaType, "INVALID_AVATAR", "La foto debe ser JPEG, PNG o WebP")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, auth.MaxAvatarBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			response.Error(w, http.StatusRequestEntityTooLarge, "AVATAR_TOO_LARGE", "La foto debe pesar como máximo 5 MiB")
			return
		}
		response.Error(w, http.StatusBadRequest, "INVALID_AVATAR", "No se pudo leer la foto")
		return
	}
	if format := validatedImageFormat(data); format == "" || "image/"+format != mediaType {
		response.Error(w, http.StatusBadRequest, "INVALID_AVATAR", "La foto debe ser JPEG, PNG o WebP válido y tener dimensiones permitidas")
		return
	}

	user, _ := middleware.Identity(r.Context())
	if err := h.avatars.Replace(r.Context(), user.ID, auth.AvatarImage{Bytes: data, MediaType: mediaType}); err != nil {
		h.avatarError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Auth) avatarError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, auth.ErrAvatarStorage) {
		response.Error(w, http.StatusServiceUnavailable, "UNAVAILABLE", "Almacenamiento de fotos no disponible")
		return
	}
	h.fail(w, r, err)
}
