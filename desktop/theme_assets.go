package main

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const themeAssetURLPrefix = "/__semantix_theme_asset/"

// themeAssetMiddleware serves theme background images through a content-addressed
// read-only route. The frontend only receives temporary URLs — never absolute paths.
//
// URL shape: /__semantix_theme_asset/{themeID}/{digest}/{filename}
// On every request the server re-validates theme id, filename, MIME, file identity
// (digest), and directory containment.
func (a *App) themeAssetMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, themeAssetURLPrefix) {
				next.ServeHTTP(w, r)
				return
			}
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			rest := strings.TrimPrefix(r.URL.Path, themeAssetURLPrefix)
			parts := strings.Split(rest, "/")
			if len(parts) != 3 {
				http.NotFound(w, r)
				return
			}
			themeID, digest, filename := parts[0], parts[1], parts[2]
			// Plugin themes (plugin:<plugin>:<theme>) serve scene images
			// straight from the installed plugin's ZIP; everything else keeps
			// the registry/library path below.
			if pluginName, pluginThemeID, ok := parsePluginThemeID(themeID); ok {
				servePluginThemeAsset(w, r, pluginName, pluginThemeID, digest, filename)
				return
			}
			if !themePackIDRe.MatchString(themeID) {
				http.NotFound(w, r)
				return
			}
			if !themePackImageRe.MatchString(filename) {
				http.NotFound(w, r)
				return
			}
			if len(digest) < 8 || len(digest) > 64 {
				http.NotFound(w, r)
				return
			}
			if isBuiltinThemeID(themeID) {
				http.NotFound(w, r)
				return
			}
			abs, err := resolveThemeImageAbs(themeID, filename)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			// Re-validate file identity against the digest embedded in the URL.
			got, err := themeFileDigest(abs)
			if err != nil || !strings.EqualFold(got, digest) {
				http.NotFound(w, r)
				return
			}
			if err := validateThemeImageFile(abs); err != nil {
				http.NotFound(w, r)
				return
			}
			f, err := os.Open(abs)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer f.Close()
			info, err := f.Stat()
			if err != nil {
				http.NotFound(w, r)
				return
			}
			mimeType := themeImageMIMEFromName(filename)
			w.Header().Set("Content-Type", mimeType)
			w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": filename}))
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "private, max-age=3600")
			http.ServeContent(w, r, filename, info.ModTime(), f)
		})
	}
}

func themeBackgroundURL(themeID, imageName string) string {
	if imageName == "" || isBuiltinThemeID(themeID) {
		return ""
	}
	abs, err := resolveThemeImageAbs(themeID, imageName)
	if err != nil {
		return ""
	}
	digest, err := themeFileDigest(abs)
	if err != nil {
		return ""
	}
	return themeAssetURLPrefix + themeID + "/" + digest + "/" + filepath.Base(imageName)
}
