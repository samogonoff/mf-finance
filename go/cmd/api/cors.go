package main

import (
	"net/http"
	"strings"
)

// withCORS — простой CORS-мидлвар. В проде список origins берётся из ENV
// (через запятую), на dev — звёздочка для удобства.
func withCORS(next http.Handler, origins []string) http.Handler {
	allowAll := len(origins) == 1 && origins[0] == "*"
	allow := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		allow[strings.TrimSpace(o)] = struct{}{}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if _, ok := allow[origin]; ok || allowAll {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")
				w.Header().Set("Access-Control-Max-Age", "600")
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
