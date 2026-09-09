package gwerr

import (
	"encoding/json"
	"net/http"
)

type envelope struct {
	Error body `json:"error"`
}

type body struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func Write(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Error: body{Code: code, Message: message}})
}
