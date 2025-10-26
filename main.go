package main

import (
	"flag"
	"fmt"
	"html"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Theme struct {
	Name                 string  `yaml:"name"`
	CSS                  string  `yaml:"css"`
	Title                string  `yaml:"title"`
	Logo                 string  `yaml:"logo"`
	ClassificationLabel  string  `yaml:"classification_label"`
	ClassificationBg     string  `yaml:"classification_bg"`
	ClassificationFg     string  `yaml:"classification_fg"`
	Transition           string  `yaml:"transition"`
	Watermark            bool    `yaml:"watermark"`
	WatermarkText        string  `yaml:"watermark_text"`
	WatermarkOpacity     float64 `yaml:"watermark_opacity"`
	WatermarkAppendDate  bool    `yaml:"watermark_append_date"`
	WatermarkMoveSeconds int     `yaml:"watermark_move_seconds"`
	FirstSlide           string  `yaml:"first_slide"`
	LastSlide            string  `yaml:"last_slide"`
}

type Config struct {
	Themes map[string]Theme `yaml:"themes"`
}

type Frontmatter struct {
	Title string `yaml:"title"`
}

var (
	markdownFile     = flag.String("file", "slides.md", "Path to markdown file")
	themeName        = flag.String("theme", "dark", "Theme name to use")
	port             = flag.String("port", "8080", "Port to serve on")
	configFile       = flag.String("config", "", "Path to themes configuration file (defaults to XDG or local)")
	orderedListRegex = regexp.MustCompile(`^(\d+)\.\s+(.+)$`)
)

func normalizeAssetPath(src string) string {
	lower := strings.ToLower(src)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "/") {
		return src
	}
	return "/assets/" + src
}

func resolveConfigPath(userSpecified string) string {
	// 1) If explicitly provided via -config, use it
	if strings.TrimSpace(userSpecified) != "" {
		return userSpecified
	}

	// 2) XDG config: $XDG_CONFIG_HOME/slides.md.yaml or ~/.config/slides.md.yaml
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if strings.TrimSpace(xdg) == "" {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			xdg = filepath.Join(home, ".config")
		}
	}
	if strings.TrimSpace(xdg) != "" {
		candidate := filepath.Join(xdg, "slides.md.yaml")
		if fileExists(candidate) {
			return candidate
		}
	}

	// 3) Local overrides in cwd
	if fileExists("slides.md.yaml") {
		return "slides.md.yaml"
	}
	if fileExists("themes.yaml") {
		return "themes.yaml"
	}

	// 4) Last resort default name
	return "themes.yaml"
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return true
	}
	return false
}

type Slide struct {
	Content template.HTML
	Number  int
}

func main() {
	flag.Parse()

	// Load themes configuration
	cfgPath := resolveConfigPath(*configFile)
	config, err := loadConfig(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Validate theme exists
	theme, exists := config.Themes[*themeName]
	if !exists {
		log.Fatalf("Theme '%s' not found in configuration", *themeName)
	}

	// Read and parse markdown
	mdContent, err := os.ReadFile(*markdownFile)
	if err != nil {
		log.Fatalf("Failed to read markdown file: %v", err)
	}

	deckTitle, body := parseFrontmatter(string(mdContent))
	slidesContent := parseMarkdown(body)

	// Augment slides with theme-provided first/last slides
	if strings.TrimSpace(theme.FirstSlide) != "" {
		slidesContent = append([]string{theme.FirstSlide}, slidesContent...)
	}
	if strings.TrimSpace(theme.LastSlide) != "" {
		slidesContent = append(slidesContent, theme.LastSlide)
	}

	// Convert markdown to HTML
	slides := make([]Slide, len(slidesContent))
	for i, slide := range slidesContent {
		slides[i] = Slide{
			Content: template.HTML(markdownToHTML(slide)),
			Number:  i + 1,
		}
	}

	// Determine page title: frontmatter > theme default
	pageTitle := theme.Title
	if strings.TrimSpace(deckTitle) != "" {
		pageTitle = deckTitle
	}
	// Determine transition (default cut)
	transition := strings.ToLower(strings.TrimSpace(theme.Transition))
	switch transition {
	case "fade", "slide", "cut":
	default:
		transition = "cut"
	}

	// HTTP handlers
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		renderSlides(w, slides, theme, pageTitle, transition)
	})

	http.HandleFunc("/style.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		io.WriteString(w, theme.CSS)
	})

	// Static assets from the markdown file directory, served under /assets/
	absPath, err := filepath.Abs(*markdownFile)
	if err == nil {
		baseDir := filepath.Dir(absPath)
		fs := http.FileServer(http.Dir(baseDir))
		http.Handle("/assets/", http.StripPrefix("/assets/", fs))
	}

	fmt.Printf("Starting server on http://localhost:%s\n", *port)
	fmt.Printf("Config: %s\n", cfgPath)
	fmt.Printf("Theme: %s\n", *themeName)
	fmt.Println("Press Ctrl+C to stop")
	log.Fatal(http.ListenAndServe(":"+*port, nil))
}

func loadConfig(filepath string) (*Config, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, err
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func parseMarkdown(content string) []string {
	// Split by horizontal rules (---) or headings
	var slides []string

	// Remove leading/trailing whitespace
	content = strings.TrimSpace(content)

	// Split by double newline followed by ---
	parts := strings.Split(content, "\n---\n")

	if len(parts) == 1 {
		// No slide breaks found, try splitting by heading boundaries
		lines := strings.Split(content, "\n")
		currentSlide := []string{}
		inCodeBlock := false

		for _, line := range lines {
			if strings.HasPrefix(line, "```") {
				inCodeBlock = !inCodeBlock
				currentSlide = append(currentSlide, line)
				continue
			}

			if !inCodeBlock && strings.HasPrefix(line, "#") {
				// New slide starting with a heading
				if len(currentSlide) > 0 {
					slides = append(slides, strings.Join(currentSlide, "\n"))
				}
				currentSlide = []string{line}
			} else {
				currentSlide = append(currentSlide, line)
			}
		}

		if len(currentSlide) > 0 {
			slides = append(slides, strings.Join(currentSlide, "\n"))
		}
	} else {
		// Split by slide breaks
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				slides = append(slides, part)
			}
		}
	}

	return slides
}

// parseFrontmatter extracts YAML frontmatter delimited by --- at the top of the file.
// Returns title (if present) and the remaining markdown body.
func parseFrontmatter(content string) (string, string) {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "---\n") && trimmed != "---" {
		return "", content
	}
	// Find closing delimiter
	parts := strings.SplitN(trimmed, "\n---\n", 2)
	if len(parts) != 2 {
		return "", content
	}
	fmText := strings.TrimPrefix(parts[0], "---\n")
	body := parts[1]

	var fm Frontmatter
	if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
		// If unmarshal fails, just return original content
		return "", content
	}
	return fm.Title, body
}

// markdownToHTML converts markdown text to HTML
func markdownToHTML(md string) string {
	if md == "" {
		return ""
	}

	lines := strings.Split(md, "\n")
	var result strings.Builder
	var inCodeBlock bool
	var inUL bool
	var inOL bool

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Handle code blocks
		if strings.HasPrefix(trimmed, "```") {
			// close any open lists before code blocks
			if inUL {
				result.WriteString("</ul>\n")
				inUL = false
			}
			if inOL {
				result.WriteString("</ol>\n")
				inOL = false
			}
			if inCodeBlock {
				result.WriteString("</code></pre>")
				inCodeBlock = false
			} else {
				result.WriteString("<pre><code>")
				inCodeBlock = true
			}
			continue
		}

		if inCodeBlock {
			result.WriteString(html.EscapeString(line))
			result.WriteString("\n")
			continue
		}

		// Headings
		if strings.HasPrefix(trimmed, "#") {
			level := 0
			for level < len(trimmed) && trimmed[level] == '#' {
				level++
			}
			if level <= 6 {
				content := strings.TrimSpace(trimmed[level:])
				content = parseInline(content)
				result.WriteString(fmt.Sprintf("<h%d>%s</h%d>\n", level, content, level))
				continue
			}
		}

		// Horizontal rules
		if trimmed == "---" || trimmed == "***" || trimmed == "___" {
			result.WriteString("<hr>\n")
			continue
		}

		// Unordered lists
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			if inOL { // close ordered list if switching types
				result.WriteString("</ol>\n")
				inOL = false
			}
			if !inUL {
				result.WriteString("<ul>\n")
				inUL = true
			}
			content := parseInline(strings.TrimPrefix(strings.TrimPrefix(trimmed, "- "), "* "))
			result.WriteString(fmt.Sprintf("<li>%s</li>\n", content))
			continue
		}

		// Ordered lists
		if match := orderedListRegex.FindStringSubmatch(trimmed); len(match) > 0 {
			if inUL { // close unordered list if switching types
				result.WriteString("</ul>\n")
				inUL = false
			}
			if !inOL {
				result.WriteString("<ol>\n")
				inOL = true
			}
			content := parseInline(match[2])
			result.WriteString(fmt.Sprintf("<li>%s</li>\n", content))
			continue
		}

		// Regular paragraph
		if trimmed != "" {
			// close any open list before paragraph
			if inUL {
				result.WriteString("</ul>\n")
				inUL = false
			}
			if inOL {
				result.WriteString("</ol>\n")
				inOL = false
			}
			content := parseInline(trimmed)
			result.WriteString(fmt.Sprintf("<p>%s</p>\n", content))
		}
	}

	if inCodeBlock {
		result.WriteString("</code></pre>")
	}
	if inUL {
		result.WriteString("</ul>\n")
	}
	if inOL {
		result.WriteString("</ol>\n")
	}

	return result.String()
}

// parseInline converts inline markdown to HTML
func parseInline(text string) string {

	// Escape entire string first to avoid injections
	text = html.EscapeString(text)

	// Protect inline code first with placeholders to avoid further processing inside
	codeRegex := regexp.MustCompile("`([^`]+)`")
	codePlaceholders := make(map[string]string)
	codeCounter := 0
	text = codeRegex.ReplaceAllStringFunc(text, func(match string) string {
		placeholder := fmt.Sprintf("__CODE%d__", codeCounter)
		codeCounter++
		parts := codeRegex.FindStringSubmatch(match)
		if len(parts) == 2 {
			codePlaceholders[placeholder] = fmt.Sprintf("<code>%s</code>", parts[1])
		} else {
			codePlaceholders[placeholder] = match
		}
		return placeholder
	})

	// Images ![alt](src)
	imageRegex := regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	text = imageRegex.ReplaceAllStringFunc(text, func(match string) string {
		parts := imageRegex.FindStringSubmatch(match)
		if len(parts) == 3 {
			alt := parts[1]
			src := normalizeAssetPath(parts[2])
			return fmt.Sprintf(`<img src="%s" alt="%s"/>`, src, alt)
		}
		return match
	})

	// Links [text](url)
	linkRegex := regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	text = linkRegex.ReplaceAllStringFunc(text, func(match string) string {
		parts := linkRegex.FindStringSubmatch(match)
		if len(parts) == 3 {
			label := parts[1]
			href := normalizeAssetPath(parts[2])
			return fmt.Sprintf(`<a href="%s">%s</a>`, href, label)
		}
		return match
	})

	// Bold **text**
	boldRegex := regexp.MustCompile(`\*\*([^*]+)\*\*`)
	text = boldRegex.ReplaceAllString(text, `<strong>$1</strong>`)

	// Italic *text*
	// Italic bounded by whitespace or start/end (no lookarounds)
	italicRegex := regexp.MustCompile(`(^|\s)\*([^*\n]+?)\*(\s|$)`)
	text = italicRegex.ReplaceAllString(text, `$1<em>$2</em>$3`)

	// Restore code placeholders
	for placeholder, replacement := range codePlaceholders {
		text = strings.ReplaceAll(text, placeholder, replacement)
	}

	return text
}

func renderSlides(w http.ResponseWriter, slides []Slide, theme Theme, pageTitle string, transition string) {
	tmpl := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}}</title>
    <link rel="stylesheet" href="/style.css">
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif;
            margin: 0;
            padding: 0;
            display: flex;
            justify-content: center;
            align-items: center;
            min-height: 100vh;
            overflow: hidden;
        }
        .slide-container {
            width: 90vw;
            max-width: 1200px;
            height: 90vh;
            position: relative;
        }
        .watermark {
            position: fixed; /* cover entire page */
            inset: 0;
            pointer-events: none;
            z-index: 5;
            /* Subtle diagonal grid lines */
            background-image: repeating-linear-gradient(
                45deg,
                rgba(0,0,0, OP) 0,
                rgba(0,0,0, OP) 1px,
                transparent 1px,
                transparent 60px
            );
        }
        .watermark-texts {
            position: fixed; /* cover entire page */
            width: 160vw; /* oversize to cover corners after rotation */
            height: 160vh;
            left: 50%;
            top: 50%;
            transform: translate(-50%, -50%) rotate(-25deg);
            display: grid;
            grid-template-columns: repeat(8, 1fr);
            grid-auto-rows: 120px;
            gap: 28px;
            opacity: OP;
            color: currentColor;
            z-index: 6;
            pointer-events: none;
        }
        .watermark-texts span {
            font-size: 36px; /* bigger, denser tiling */
            font-weight: 700;
            letter-spacing: 0.14em;
            text-transform: uppercase;
            white-space: nowrap;
            justify-self: center;
            align-self: center;
        }
        .slide {
            display: none;
            padding: 60px;
            box-sizing: border-box;
            overflow-y: auto;
        }
        .slide.active { display: block; }

        /* Transitions */
        /* FADE: overlay slides and cross-fade */
        .transition-fade { position: relative; }
        .transition-fade .slide {
            display: block; /* override base */
            position: absolute;
            top: 0; left: 0; right: 0; bottom: 0;
            opacity: 0;
            transition: opacity 220ms ease;
            pointer-events: none;
            z-index: 1;
        }
        .transition-fade .slide.active {
            opacity: 1;
            pointer-events: auto;
        }

        /* SLIDE: support direction-aware animations */
        .transition-slide { position: relative; overflow: hidden; }
        .transition-slide .slide {
            display: block; /* override base */
            position: absolute;
            top: 0; left: 0; right: 0; bottom: 0;
            transform: translateX(100%);
            transition: transform 260ms ease;
            pointer-events: none;
            z-index: 1;
        }
        .transition-slide .slide.pre-right { transform: translateX(100%); }
        .transition-slide .slide.pre-left  { transform: translateX(-100%); }
        .transition-slide .slide.active    { transform: translateX(0); pointer-events: auto; }
        .transition-slide .slide.exiting-left { transform: translateX(-100%); }
        .transition-slide .slide.exiting-right { transform: translateX(100%); }

        .transition-cut .slide { }
        .controls {
            position: fixed;
            bottom: 20px;
            left: 50%;
            transform: translateX(-50%);
            display: flex;
            gap: 10px;
            z-index: 1000;
        }
        button {
            padding: 10px 20px;
            cursor: pointer;
            border: 1px solid currentColor;
            border-radius: 4px;
            font-size: 14px;
            transition: opacity 0.2s;
        }
        button:hover {
            opacity: 0.7;
        }
        code {
            padding: 2px 6px;
            border-radius: 3px;
        }
        pre {
            padding: 16px;
            border-radius: 6px;
            overflow-x: auto;
        }
        pre code {
            padding: 0;
        }
        img {
            max-width: 100%;
            height: auto;
            display: block;
            margin: 16px 0;
        }
        h1 { font-size: 2.5em; }
        h2 { font-size: 2em; }
        h3 { font-size: 1.5em; }
        h4 { font-size: 1.25em; }
        .slide-counter {
            position: fixed;
            top: 20px;
            right: 20px;
            font-size: 14px;
            opacity: 0.6;
        }
        .deck-title {
            position: fixed;
            top: 20px;
            left: 20px;
            font-size: 14px;
            opacity: 0.8;
        }
        .theme-logo {
            position: absolute;
            top: 16px;
            right: 16px;
            max-width: 140px;
            max-height: 60px;
            object-fit: contain;
            opacity: 0.9;
            pointer-events: none;
            z-index: 2;
        }
        .classification {
            position: fixed;
            top: 20px;
            left: 50%;
            transform: translateX(-50%);
            display: inline-flex;
            align-items: center;
            justify-content: center;
            font-weight: 600;
            font-size: 13px;
            letter-spacing: 0.08em;
            padding: 4px 10px;
            border-radius: 999px;
            z-index: 1200;
            pointer-events: none;
        }
    </style>
</head>
<body>
    {{if .Classification.Label}}
    <div class="classification" style="background: {{.Classification.Bg}}; color: {{.Classification.Fg}}">{{.Classification.Label}}</div>
    {{end}}
    <div class="deck-title">{{.DeckTitle}}</div>
    <div class="slide-counter">
        <span id="current">1</span> / {{len .Slides}}
    </div>
    <div class="slide-container transition-{{.Transition}}">
        {{if .Watermark.Enabled}}
        <div class="watermark"></div>
        <div class="watermark-texts" id="wm-texts">
            {{/* Render a tiled grid of texts */}}
            {{range .Watermark.Repeat}}<span class="wm-item">{{$.Watermark.Text}}</span>{{end}}
        </div>
        {{end}}
        {{if .Logo}}
        <img class="theme-logo" src="{{.Logo}}" alt="Logo"/>
        {{end}}
        {{range .Slides}}
        <div class="slide {{if eq .Number 1}}active{{else}}pre-right{{end}}" id="slide-{{.Number}}">
            {{.Content}}
        </div>
        {{end}}
    </div>
    <div class="controls">
        <button onclick="previousSlide()">← Previous</button>
        <button onclick="nextSlide()">Next →</button>
    </div>
    <script>
        let currentSlide = 0;
        const slides = document.querySelectorAll('.slide');
        const totalSlides = {{len .Slides}};

        function showSlide(n, dir) {
            const container = document.querySelector('.slide-container');
            const transition = container.className.includes('transition-') ?
                container.className.match(/transition-([a-z]+)/)[1] : 'cut';

            const previousIndex = currentSlide;
            const previous = slides[previousIndex];
            previous.classList.remove('exiting');

            currentSlide = n;
            if (currentSlide >= totalSlides) currentSlide = 0;
            if (currentSlide < 0) currentSlide = totalSlides - 1;

            const next = slides[currentSlide];

            if (previous === next) {
                // Ensure visible on first render
                next.classList.add('active');
                document.getElementById('current').textContent = currentSlide + 1;
                return;
            }

            if (transition === 'fade') {
                // Activate next first, then hide previous after tick
                next.classList.add('active');
                setTimeout(() => {
                    previous.classList.remove('active');
                }, 0);
            } else if (transition === 'slide') {
                // Direction-aware slide: use provided dir (1 forward, -1 backward)
                const forward = (dir || 0) >= 0;
                // Prepare next slide off-screen in the correct direction
                next.classList.remove('pre-left','pre-right','exiting-left','exiting-right');
                previous.classList.remove('pre-left','pre-right','exiting-left','exiting-right');
                if (forward) {
                    next.classList.add('pre-right');
                    // force reflow
                    void next.offsetWidth;
                    previous.classList.add('exiting-left');
                } else {
                    next.classList.add('pre-left');
                    void next.offsetWidth;
                    previous.classList.add('exiting-right');
                }
                next.classList.add('active');
                // After transition ends, clean up previous
                previous.addEventListener('transitionend', function handler() {
                    previous.classList.remove('active','exiting-left','exiting-right');
                    previous.removeEventListener('transitionend', handler);
                });
                // Clean pre-* class on next after it finishes activating
                next.addEventListener('transitionend', function cleanNext(e) {
                    if (e.propertyName === 'transform') {
                        next.classList.remove('pre-left','pre-right');
                        next.removeEventListener('transitionend', cleanNext);
                    }
                });
            } else {
                // cut
                previous.classList.remove('active');
                next.classList.add('active');
            }
            document.getElementById('current').textContent = currentSlide + 1;
        }

        function nextSlide() {
            showSlide(currentSlide + 1, 1);
        }

        function previousSlide() {
            showSlide(currentSlide - 1, -1);
        }

        // Keyboard navigation
        document.addEventListener('keydown', function(e) {
            if (e.key === 'ArrowRight' || e.key === ' ') {
                nextSlide();
            } else if (e.key === 'ArrowLeft') {
                previousSlide();
            }
        });

        // Watermark drift animation
        (function() {
            const interval = {{.Watermark.MoveMs}};
            if (interval && interval > 0) {
                let offset = 0;
                setInterval(() => {
                    offset = (offset + 12) % 96;
                    const wm = document.getElementById('wm-texts');
                    if (wm) {
                        wm.style.transform = 'translate(-50%, -50%) rotate(-25deg) translate(' + offset + 'px, ' + offset + 'px)';
                    }
                }, interval);
            }
        })();

        // Initialize
        showSlide(0, 1);
    </script>
</body>
</html>`

	t := template.Must(template.New("slides").Parse(tmpl))
	data := struct {
		Title          string
		DeckTitle      string
		Logo           string
		Classification struct {
			Label string
			Bg    string
			Fg    string
		}
		Transition string
		Watermark  struct {
			Enabled bool
			Text    string
			Opacity string
			Repeat  []int
			MoveMs  int
		}
		Slides []Slide
	}{}

	data.Title = pageTitle
	data.DeckTitle = pageTitle
	data.Logo = normalizeAssetPath(theme.Logo)
	data.Classification.Label = theme.ClassificationLabel
	// Provide sensible defaults if theme values are empty
	if strings.TrimSpace(theme.ClassificationBg) == "" {
		data.Classification.Bg = "#5e81ac"
	} else {
		data.Classification.Bg = theme.ClassificationBg
	}
	if strings.TrimSpace(theme.ClassificationFg) == "" {
		data.Classification.Fg = "#ffffff"
	} else {
		data.Classification.Fg = theme.ClassificationFg
	}
	data.Transition = transition
	// Watermark
	if theme.Watermark {
		data.Watermark.Enabled = true
		text := strings.TrimSpace(theme.WatermarkText)
		if text == "" {
			text = data.DeckTitle
		}
		if theme.WatermarkAppendDate {
			text = fmt.Sprintf("%s — %s", text, time.Now().Format("2006-01-02"))
		}
		data.Watermark.Text = text
		// clamp opacity
		op := theme.WatermarkOpacity
		if op <= 0 || op > 1 {
			op = 0.08
		}
		data.Watermark.Opacity = fmt.Sprintf("%.2f", op)
		// prepare repetition tiles
		rep := 96
		data.Watermark.Repeat = make([]int, rep)
		for i := 0; i < rep; i++ {
			data.Watermark.Repeat[i] = i
		}
		if theme.WatermarkMoveSeconds > 0 {
			data.Watermark.MoveMs = theme.WatermarkMoveSeconds * 1000
		}
	}
	data.Slides = slides

	// Inject opacity constant into CSS (simple string replace) after data populated
	if theme.Watermark {
		op := data.Watermark.Opacity
		if op == "" {
			op = "0.08"
		}
		tmpl = strings.ReplaceAll(tmpl, "OP", op)
		t = template.Must(template.New("slides").Parse(tmpl))
	}

	err := t.Execute(w, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
