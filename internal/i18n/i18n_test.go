package i18n

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/text/language"
)

func TestMatchLanguage(t *testing.T) {
	tests := []struct {
		accept   string
		expected language.Tag
	}{
		{"en-US,en;q=0.9", language.English},
		{"de-DE,de;q=0.9", language.German},
		{"fr-FR", language.English}, // Fallback
		{"", language.English},      // Empty
	}

	for _, tt := range tests {
		got := MatchLanguage(tt.accept)
		// We only check the base language for simplicity here, as exact tag matching can be tricky with regions
		base, _ := got.Base()
		exp, _ := tt.expected.Base()
		assert.Equal(t, exp, base, "Accept: %s", tt.accept)
	}
}

func TestMiddleware(t *testing.T) {
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := GetPrinter(r.Context())
		assert.NotNil(t, p)
		// We can't easily check the language of the printer without using it, 
		// but we can verify it exists in context.
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Language", "de")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
}
