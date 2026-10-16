package gwerr

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type tagKey struct{}

func WithTag(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), tagKey{}, new(string)))
}

func SetTag(r *http.Request, tag string) {
	p, ok := r.Context().Value(tagKey{}).(*string)
	if !ok {
		return
	}
	if *p == "" {
		*p = tag
		return
	}
	for _, existing := range strings.Split(*p, ",") {
		if existing == tag {
			return
		}
	}
	*p += "," + tag
}

func Tag(r *http.Request) string {
	if p, ok := r.Context().Value(tagKey{}).(*string); ok {
		return *p
	}
	return ""
}

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
