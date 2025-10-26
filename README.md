# Slides.md

A minimalistic markdown slide presentation tool written in Go. Serve any markdown file as beautiful, themeable slides in your browser.

## Features

- 🎨 **Multiple IDE-inspired themes**: Light, Dark, Solarized, Dracula, Nord, One Dark
- 📝 **Full markdown support**: Headings, code blocks, lists, links, and more
- ⌨️ **Keyboard navigation**: Arrow keys and spacebar
- 🎯 **Minimalistic design**: Clean, distraction-free interface
- ⚙️ **Configurable**: YAML-based theme configuration
- 🚀 **Fast**: Lightweight Go binary

## Installation

```bash
# Clone the repository
git clone https://github.com/LGDan/slides.md.git
cd slides.md

# Download dependencies
go mod download

# Build
go build -o slides main.go
```

## Usage

### Basic Usage

```bash
# Serve slides.md with default dark theme
./slides

# Specify a different markdown file
./slides -file=my-slides.md

# Use a different theme
./slides -theme=solarized-light

# Custom port
./slides -port=3000
```

### Command Line Options

- `-file`: Path to markdown file (default: `slides.md`)
- `-theme`: Theme name (default: `dark`)
- `-port`: Server port (default: `8080`)
- `-config`: Path to themes configuration file (default: `themes.yaml`)

### Available Themes

- `light` - Bright, clean GitHub-style theme
- `dark` - Dark mode with subtle colors (default)
- `solarized-light` - Solarized light theme
- `solarized-dark` - Solarized dark theme
- `dracula` - Dracula theme
- `nord` - Nord theme
- `one-dark` - One Dark Pro theme

## Creating Slides

Slides are automatically detected in your markdown file using:

1. **Horizontal rules**: Use `---` to separate slides
2. **Headings**: Each `#` heading starts a new slide

### Example

```markdown
# Welcome to Slides.md

A minimalistic presentation tool

---

## Getting Started

Run with:
\`\`\`bash
./slides -file=example.md
\`\`\`

---

## Features

- Markdown support
- Multiple themes
- Keyboard navigation
```

## Customizing Themes

Themes are defined in `themes.yaml`. To create a custom theme:

```yaml
themes:
  my-theme:
    name: My Theme
    title: Slides
    css: |
      body {
        background: #your-color;
        color: #your-text-color;
      }
      .slide {
        background: #your-color;
        color: #your-text-color;
      }
      button {
        background: #your-button-bg;
        color: #your-button-text;
      }
      # ... more CSS
```

## Navigation

- **Right Arrow** or **Space**: Next slide
- **Left Arrow**: Previous slide
- **Click buttons**: Navigate manually

## Example

Try running with the included example:

```bash
./slides -file=example-slides.md -theme=dracula
```

Then open http://localhost:8080 in your browser.

## License

MIT