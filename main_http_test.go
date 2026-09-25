package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/WindowsSov8forUs/glyccat/config"
	"github.com/go-chi/chi/v5"
)

type testRootRoutes struct{}

func (testRootRoutes) RegisterRootRoutes(router chi.Router) {
	router.Get("/qqbot", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
}

func TestQQWebhookListenerAndResponseHeaders(t *testing.T) {
	conf := config.DefaultConfig()
	conf.Account.WebHook.Host = "::1"
	conf.Account.WebHook.Port = 5141
	webhook := buildQQWebhookServer(conf, testRootRoutes{}, "v1", "GlycCat/test")
	if webhook == nil || webhook.Addr != "[::1]:5141" || webhook.ReadHeaderTimeout == 0 {
		t.Fatalf("webhook listener = %#v", webhook)
	}
	response := httptest.NewRecorder()
	webhook.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/qqbot", nil))
	if response.Code != http.StatusNoContent || response.Header().Get("Server") != "GlycCat/test" ||
		response.Header().Get("X-Satori-Protocol") != "v1" {
		t.Fatalf("response = %d %#v", response.Code, response.Result().Header)
	}
}

func TestQQWebhookSharesSatoriListener(t *testing.T) {
	conf := config.DefaultConfig()
	conf.Account.WebHook.Host = conf.Satori.Server.Host
	conf.Account.WebHook.Port = conf.Satori.Server.Port
	if server := buildQQWebhookServer(conf, testRootRoutes{}, "v1", "GlycCat/test"); server != nil {
		t.Fatalf("unexpected separate listener: %#v", server)
	}
}
