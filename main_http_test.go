package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResponseHeaderMiddleware(t *testing.T) {
	handler := responseHeaderMiddleware("v1", "GlycCat/test")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusNoContent || response.Header().Get("Server") != "GlycCat/test" ||
		response.Header().Get("X-Satori-Protocol") != "v1" {
		t.Fatalf("response = %#v", response.Result().Header)
	}
}
