package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerServesIndexAndClientRoutes(t *testing.T) {
	handler := Handler()
	for _, requestPath := range []string{"/", "/settings/profile"} {
		t.Run(requestPath, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, requestPath, nil)
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
			}
			if response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
				t.Fatalf("content type = %q, want HTML", response.Header().Get("Content-Type"))
			}
		})
	}
}

func TestHandlerServesAnEmbeddedAsset(t *testing.T) {
	assets, err := fs.ReadDir(embedded, "dist/assets")
	if err != nil {
		t.Skip("frontend assets have not been staged")
	}
	assetName := ""
	for _, asset := range assets {
		if !asset.IsDir() {
			assetName = asset.Name()
			break
		}
	}
	if assetName == "" {
		t.Skip("frontend assets have not been staged")
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/assets/"+assetName, nil)
	Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Body.Len() == 0 {
		t.Fatal("asset response was empty")
	}
}

func TestHandlerDoesNotFallbackForAPIOrMissingAssets(t *testing.T) {
	handler := Handler()
	for _, requestPath := range []string{"/api/missing", "/assets", "/assets/missing.js", "/favicon.ico"} {
		t.Run(requestPath, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, requestPath, nil)
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
			}
		})
	}
}
