package localization

import (
	"embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	LanguageEnglish = "eng"
)

//go:embed langs/*.json
var catalogsFS embed.FS

type Localizer struct {
	language string
	catalog  map[string]string
	fallback map[string]string
}

func DetectLanguage(values ...string) string {
	for _, value := range values {
		normalized := normalizeLanguage(value)
		if normalized != "" {
			return normalized
		}
	}
	return LanguageEnglish
}

func Default() *Localizer {
	return MustNew(LanguageEnglish)
}

func MustNew(language string) *Localizer {
	loc, err := New(language)
	if err != nil {
		panic(err)
	}
	return loc
}

func New(language string) (*Localizer, error) {
	resolved := DetectLanguage(language)
	fallback, err := loadCatalog(LanguageEnglish)
	if err != nil {
		return nil, err
	}

	catalog := fallback
	if resolved != LanguageEnglish {
		catalog, err = loadCatalog(resolved)
		if err != nil {
			return nil, err
		}
	}

	return &Localizer{
		language: resolved,
		catalog:  catalog,
		fallback: fallback,
	}, nil
}

func (l *Localizer) Language() string {
	if l == nil {
		return LanguageEnglish
	}
	return l.language
}

func (l *Localizer) Text(key string) string {
	if l == nil {
		return Default().Text(key)
	}
	if value, ok := l.catalog[key]; ok {
		return value
	}
	if value, ok := l.fallback[key]; ok {
		return value
	}
	return key
}

func (l *Localizer) Format(key string, args ...any) string {
	return fmt.Sprintf(l.Text(key), args...)
}

func loadCatalog(language string) (map[string]string, error) {
	path := filepath.ToSlash(filepath.Join("langs", language+".json"))
	payload, err := catalogsFS.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s catalog: %w", language, err)
	}

	catalog := make(map[string]string)
	if err = json.Unmarshal(payload, &catalog); err != nil {
		return nil, fmt.Errorf("decode %s catalog: %w", language, err)
	}
	return catalog, nil
}

func normalizeLanguage(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return ""
	}

	trimmed = strings.ReplaceAll(trimmed, "-", "_")
	switch {
	case trimmed == "en", trimmed == "eng", strings.HasPrefix(trimmed, "en_"):
		return LanguageEnglish
	default:
		return LanguageEnglish
	}
}
