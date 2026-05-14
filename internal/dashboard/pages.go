package dashboard

import (
	"embed"
	"html/template"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"
)

//go:embed templates/*.html templates/partials/*.html
var templateFS embed.FS

type PageHandler struct {
	templates map[string]*template.Template
	mu        sync.RWMutex
}

func NewPageHandler() *PageHandler {
	h := &PageHandler{templates: make(map[string]*template.Template)}
	h.loadTemplates()
	return h
}

func (h *PageHandler) loadTemplates() {
	layoutBytes, _ := templateFS.ReadFile("templates/layout.html")
	layoutStr := string(layoutBytes)

	pages := []string{"login", "register", "dashboard"}
	for _, p := range pages {
		pageBytes, err := templateFS.ReadFile("templates/" + p + ".html")
		if err != nil {
			continue
		}
		tmpl, err := template.New("layout").Parse(layoutStr)
		if err != nil {
			continue
		}
		tmpl, err = tmpl.Parse(string(pageBytes))
		if err != nil {
			continue
		}
		h.templates[p] = tmpl
	}

	// Load partials (standalone fragments for HTMX)
	entries, _ := templateFS.ReadDir("templates/partials")
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".html")
		data, err := templateFS.ReadFile("templates/partials/" + e.Name())
		if err != nil {
			continue
		}
		tmpl, err := template.New(name).Parse(string(data))
		if err != nil {
			continue
		}
		h.templates["partial:"+name] = tmpl
	}
}

func (h *PageHandler) render(w io.Writer, name string, data interface{}) error {
	h.mu.RLock()
	tmpl, ok := h.templates[name]
	h.mu.RUnlock()

	if !ok {
		return nil
	}
	return tmpl.Execute(w, data)
}

type pageData struct {
	CurrentPage string
	Verified    bool
	Error       string
}

func (h *PageHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	data := pageData{
		Verified: r.URL.Query().Get("verified") == "true",
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.render(w, "login", data)
}

func (h *PageHandler) RegisterPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.render(w, "register", nil)
}

func (h *PageHandler) DashboardPage(w http.ResponseWriter, r *http.Request) {
	// Determine current page from URL
	current := "overview"
	p := strings.TrimPrefix(r.URL.Path, "/dashboard")
	p = strings.TrimPrefix(p, "/")
	if p != "" {
		current = path.Base(p)
	}
	data := pageData{
		CurrentPage: current,
		Verified:    r.URL.Query().Get("verified") == "true",
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.render(w, "dashboard", data)
}

func (h *PageHandler) Partial(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = h.render(w, "partial:"+name, nil)
	}
}
