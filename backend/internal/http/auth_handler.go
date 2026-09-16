package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/common"
)

type registerRequest struct {
	Name              string `json:"name"`
	Phone             string `json:"phone"`
	PaymentIdentifier string `json:"paymentIdentifier"`
	Password          string `json:"password"`
	Role              string `json:"role"`
}

type loginRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

func (h *Handler) register(writer http.ResponseWriter, request *http.Request) {
	var input registerRequest
	if err := decodeJSON(writer, request, &input); err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "request body is invalid", http.StatusBadRequest))
		return
	}
	user, err := h.auth.Register(request.Context(), auth.RegisterInput{Name: input.Name, Phone: input.Phone, PaymentIdentifier: input.PaymentIdentifier, Password: input.Password, Role: input.Role})
	if err != nil {
		writeAuthError(writer, request, err)
		return
	}
	writeData(writer, http.StatusCreated, request, user)
}

func (h *Handler) login(writer http.ResponseWriter, request *http.Request) {
	var input loginRequest
	if err := decodeJSON(writer, request, &input); err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "request body is invalid", http.StatusBadRequest))
		return
	}
	token, user, err := h.auth.Login(request.Context(), input.Identifier, input.Password, time.Now())
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_CREDENTIALS", "invalid credentials", http.StatusUnauthorized))
		return
	}
	writeData(writer, http.StatusOK, request, map[string]any{"token": token, "user": user})
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(writer, request.Body, 16<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("request contains multiple JSON values")
	}
	return nil
}

func writeAuthError(writer http.ResponseWriter, request *http.Request, err error) {
	var apiErr *common.APIError
	if errors.As(err, &apiErr) {
		writeAPIError(writer, request, apiErr)
		return
	}
	switch {
	case errors.Is(err, auth.ErrDuplicateUser):
		writeAPIError(writer, request, common.NewAPIError("USER_ALREADY_EXISTS", "user already exists", http.StatusConflict))
	case errors.Is(err, auth.ErrBankNotFound):
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "registration is temporarily unavailable", http.StatusServiceUnavailable))
	default:
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "internal server error", http.StatusInternalServerError))
	}
}
