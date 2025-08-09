package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var tmpls = template.Must(template.ParseGlob("templates/*.html"))

const (
	maxUploadBytes = 200 * 1024 * 1024 // 200MB
	uploadDir      = "uploads"
)

func main() {
	// Ensure uploads dir exists
	_ = os.MkdirAll(uploadDir, 0o755)

	// Routes
	http.HandleFunc("/", home)
	http.HandleFunc("/tools/mov-to-mp4", movToMp4)
	http.HandleFunc("/tools/mov-to-gif", movToGif)

	// Serve converted files
	http.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadDir))))

	// SEO/static files
	http.Handle("/sitemap.xml", http.FileServer(http.Dir(".")))
	http.Handle("/robots.txt", http.FileServer(http.Dir(".")))

	// Port (Render provides PORT)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Println("Listening on :" + port)
	_ = http.ListenAndServe(":"+port, nil)
}
func home(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/tools/mov-to-mp4", http.StatusFound)
}

func movToMp4(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		render(w, "mov_to_mp4.html", map[string]any{
			"Title":        "Convert MOV to MP4 | Stacking.app",
			"Description":  "Free online MOV to MP4 converter.",
			"Year":         time.Now().Year(),
			"ContentBlock": "content_mp4",
		})
	case http.MethodPost:
		outName, err := handleUploadAndConvert(w, r, "mp4")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		render(w, "mov_to_mp4.html", map[string]any{
			"Title":        "Convert MOV to MP4 | Stacking.app",
			"Description":  "Free online MOV to MP4 converter.",
			"Year":         time.Now().Year(),
			"OutputFile":   outName,
			"ContentBlock": "content_mp4",
		})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func movToGif(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		render(w, "mov_to_gif.html", map[string]any{
			"Title":        "Convert MOV to GIF | Stacking.app",
			"Description":  "Free online MOV to GIF converter.",
			"Year":         time.Now().Year(),
			"ContentBlock": "content_gif",
		})
	case http.MethodPost:
		outName, err := handleUploadAndConvert(w, r, "gif")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		render(w, "mov_to_gif.html", map[string]any{
			"Title":        "Convert MOV to GIF | Stacking.app",
			"Description":  "Free online MOV to GIF converter.",
			"Year":         time.Now().Year(),
			"OutputFile":   outName,
			"ContentBlock": "content_gif",
		})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func render(w http.ResponseWriter, contentTemplate string, data map[string]any) {
	tmpl := template.Must(template.ParseFiles("templates/base.html", "templates/"+contentTemplate))
	err := tmpl.ExecuteTemplate(w, "base.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ----- Upload + Convert helpers -----

func handleUploadAndConvert(w http.ResponseWriter, r *http.Request, target string) (string, error) {
	// Limit request size (prevents huge uploads from eating RAM)
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+10<<20) // a bit extra for form overhead
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		return "", errors.New("file too large (limit ~200MB)")
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		return "", errors.New("missing file field 'video'")
	}
	defer file.Close()

	// Basic validation: require .mov
	if !strings.EqualFold(filepath.Ext(header.Filename), ".mov") {
		return "", errors.New("only .mov files are allowed")
	}

	// Save the uploaded file to disk
	inPath, baseName, err := saveUpload(file, header)
	if err != nil {
		return "", err
	}

	// Build output file name
	outName := baseName + "." + target
	outPath := filepath.Join(uploadDir, outName)

	// Run ffmpeg conversion
	if err := runFFmpeg(inPath, outPath, target); err != nil {
		return "", fmt.Errorf("conversion failed: %w", err)
	}

	return outName, nil
}

func saveUpload(file multipart.File, header *multipart.FileHeader) (fullPath, baseName string, err error) {
	// Sanitize and uniquify file name
	name := strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
	name = sanitize(name)
	if name == "" {
		name = "video"
	}
	base := name + "-" + randHex(6) // avoid collisions
	full := filepath.Join(uploadDir, base+".mov")

	dst, err := os.Create(full)
	if err != nil {
		return "", "", errors.New("could not create file on server")
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		return "", "", errors.New("failed saving uploaded file")
	}
	return full, base, nil
}

func runFFmpeg(inputPath, outputPath, target string) error {
	// Ensure we overwrite outputs without prompting (-y)
	args := []string{"-y", "-i", inputPath}

	switch target {
	case "mp4":
		// Good defaults: web-friendly, fast-ish, reasonable quality
		args = append(args,
			"-vcodec", "libx264",
			"-preset", "veryfast",
			"-crf", "23",
			"-acodec", "aac",
			"-b:a", "128k",
			"-movflags", "+faststart",
			outputPath,
		)
	case "gif":
		// Simple, decent quality; you can improve later with palettegen/paletteuse two-pass
		args = append(args,
			"-vf", "fps=10,scale=480:-1:flags=lanczos",
			outputPath,
		)
	default:
		return errors.New("unsupported target format")
	}

	cmd := exec.Command("ffmpeg", args...)
	// Optional: uncomment for debugging
	// cmd.Stdout = os.Stdout
	// cmd.Stderr = os.Stderr

	return cmd.Run()
}

func sanitize(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	// Keep alnum, dash, underscore; drop others
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func randHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
