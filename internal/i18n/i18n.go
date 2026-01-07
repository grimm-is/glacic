package i18n

import (
	"context"
	"os"
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// DefaultLang is the fallback language
var DefaultLang = language.English

// SupportedLangs are the languages we support
var SupportedLangs = []language.Tag{
	language.English,
	language.German, // For testing/example
}

var matcher = language.NewMatcher(SupportedLangs)

type contextKey struct{}

// printerKey is the key used to store the printer in the context
var printerKey = contextKey{}

// MatchLanguage returns the best matching language for the given tags
func MatchLanguage(acceptLang string) language.Tag {
	tags, _, _ := language.ParseAcceptLanguage(acceptLang)
	tag, _, _ := matcher.Match(tags...)
	return tag
}

// NewPrinter returns a message printer for the given language
func NewPrinter(tag language.Tag) *message.Printer {
	return message.NewPrinter(tag)
}

// WithPrinter returns a new context with the printer injected
func WithPrinter(ctx context.Context, p *message.Printer) context.Context {
	return context.WithValue(ctx, printerKey, p)
}

// GetPrinter returns the printer from the context, or a default one
func GetPrinter(ctx context.Context) *message.Printer {
	p, ok := ctx.Value(printerKey).(*message.Printer)
	if !ok {
		return message.NewPrinter(DefaultLang)
	}
	return p
}

// NewCLIPrinter returns a printer for the system's locale (from env vars)
func NewCLIPrinter() *message.Printer {
	lang := os.Getenv("LC_ALL")
	if lang == "" {
		lang = os.Getenv("LANG")
	}
	if lang == "" {
		return message.NewPrinter(DefaultLang)
	}

	// Strip encoding (e.g. .UTF-8) if present
	if i := strings.Index(lang, "."); i != -1 {
		lang = lang[:i]
	}

	
	// Handle format like "en_US.UTF-8"
	// language.Parse usually handles simple tags.
	// We'll trust MatchLanguage which uses our supported matcher.
	// But commonly env vars are not HTTP headers.
	// Let's try simple Parse first.
	tag, err := language.Parse(lang)
	if err != nil {
		// Try matching against our supported list
		tag = MatchLanguage(lang)
	} else {
		// Use matcher to map "en-US" -> "en" if that's what we support
		tag, _, _ = matcher.Match(tag)
	}

	return message.NewPrinter(tag)
}
