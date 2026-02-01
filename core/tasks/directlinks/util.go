package directlinks

import (
	"fmt"
	"mime"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// ==========================================
// FILENAME PARSING & INTELLIGENCE
// ==========================================

// ParseFilename is the smart entry point.
// It extracts, decodes, sanitizes, and ensures the extension is correct.
func ParseFilename(contentDisposition, rawURL, contentType string) string {
	var name string

	// 1. Try Content-Disposition (Most Authoritative)
	if contentDisposition != "" {
		name = parseFilenameHeader(contentDisposition)
	}

	// 2. Fallback to URL if Header failed or gave generic name
	if name == "" || isGenericName(name) {
		urlName := parseFilenameFromURL(rawURL)
		if urlName != "" {
			// Logic: If header gave us "download.php" but URL is "cool-video.mp4", prefer URL
			if name == "" || (filepath.Ext(name) == "" && filepath.Ext(urlName) != "") {
				name = urlName
			}
		}
	}

	// 3. Final Fallback: Random name if everything failed
	if name == "" {
		name = fmt.Sprintf("download_%d", time.Now().Unix())
	}

	// 4. Sanitize (CRITICAL SECURITY STEP)
	// This prevents directory traversal and OS-level filename errors
	name = SanitizeFilename(name)

	// 5. Smart Extension Check
	// If file is named "video" but Content-Type is "video/mp4", rename to "video.mp4"
	name = ensureExtension(name, contentType)

	return name
}

// isGenericName checks if the filename is useless/generic (e.g. "index.html", "get")
func isGenericName(name string) bool {
	lower := strings.ToLower(name)
	base := filepath.Base(lower)
	// Remove extension to check base name
	base = strings.TrimSuffix(base, filepath.Ext(base))

	genericNames := []string{"index", "download", "default", "file", "get", "api", "stream", "video", "watch"}
	for _, g := range genericNames {
		if base == g {
			return true
		}
	}
	return false
}

// parseFilenameHeader extracts filename from Content-Disposition header
func parseFilenameHeader(contentDisposition string) string {
	// 1. RFC 5987 (filename*=) - Highest Priority (Modern Standards)
	if filename := parseFilenameExtended(contentDisposition); filename != "" {
		return filename
	}

	// 2. Standard MIME parsing
	_, params, err := mime.ParseMediaType(contentDisposition)
	if err == nil {
		if filename := params["filename"]; filename != "" {
			return decodeFilenameParam(filename)
		}
	}

	// 3. Manual Fallback (Dirty Parsing for legacy servers)
	return parseFilenameFallback(contentDisposition)
}

// parseFilenameExtended parses RFC 5987/RFC 2231
func parseFilenameExtended(cd string) string {
	lower := strings.ToLower(cd)
	idx := strings.Index(lower, "filename*=")
	if idx == -1 {
		return ""
	}

	value := cd[idx+len("filename*="):]
	if endIdx := strings.Index(value, ";"); endIdx != -1 {
		value = value[:endIdx]
	}
	value = strings.TrimSpace(value)

	// Handle format: UTF-8''encoded_value
	parts := strings.SplitN(value, "''", 2)
	if len(parts) == 2 {
		if decoded, err := url.QueryUnescape(parts[1]); err == nil {
			return decoded
		}
	}

	// Handle format: charset'lang'encoded_value
	parts = strings.SplitN(value, "'", 3)
	if len(parts) >= 3 {
		if decoded, err := url.QueryUnescape(parts[2]); err == nil {
			return decoded
		}
	}

	return ""
}

// parseFilenameFallback manually parses filename= when mime parser fails
func parseFilenameFallback(cd string) string {
	lower := strings.ToLower(cd)
	idx := strings.Index(lower, "filename=")
	if idx == -1 {
		return ""
	}

	value := cd[idx+len("filename="):]
	if endIdx := strings.Index(value, ";"); endIdx != -1 {
		value = value[:endIdx]
	}
	value = strings.TrimSpace(value)

	// Strip quotes
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
	}

	return decodeFilenameParam(value)
}

// ==========================================
// DECODING & ENCODING
// ==========================================

func decodeFilenameParam(filename string) string {
	// MIME Word Decoder (=?UTF-8?B?...)
	if strings.HasPrefix(filename, "=?") {
		decoder := new(mime.WordDecoder)
		normalized := strings.Replace(filename, "UTF8", "UTF-8", 1) // Fix common server bug
		if decoded, err := decoder.Decode(normalized); err == nil {
			return decoded
		}
	}

	decoded := tryUrlQueryUnescape(filename)

	// Fallback to GBK if invalid UTF-8 (Legacy Chinese Servers)
	if !utf8.ValidString(decoded) {
		if gbkDecoded := tryDecodeGBK(decoded); gbkDecoded != "" {
			return gbkDecoded
		}
	}

	// Force clean invalid UTF-8 sequences if all else fails
	if !utf8.ValidString(decoded) {
		return strings.ToValidUTF8(decoded, "_")
	}

	return decoded
}

func tryUrlQueryUnescape(s string) string {
	if decoded, err := url.QueryUnescape(s); err == nil {
		return decoded
	}
	return s
}

var gbkDecoder = simplifiedchinese.GBK.NewDecoder()

func tryDecodeGBK(s string) string {
	if len(s) == 0 {
		return ""
	}
	decoded, err := gbkDecoder.Bytes([]byte(s))
	if err != nil {
		return ""
	}
	result := string(decoded)
	if utf8.ValidString(result) {
		return result
	}
	return ""
}

// ==========================================
// SANITIZATION & SECURITY (CRITICAL)
// ==========================================

var (
	illegalNameChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]`)
	reservedNames    = regexp.MustCompile(`^(?i)(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])$`)
)

// SanitizeFilename ensures the filename is safe for the filesystem.
// It prevents directory traversal and removes illegal characters.
func SanitizeFilename(name string) string {
	// 1. URL Decode just in case
	if res, err := url.QueryUnescape(name); err == nil {
		name = res
	}

	// 2. Remove Path Separators (Prevent Directory Traversal attacks)
	name = filepath.Base(name)
	if name == "." || name == "/" || name == "\\" {
		name = "download_file"
	}

	// 3. Replace Illegal Characters with Underscore
	name = illegalNameChars.ReplaceAllString(name, "_")

	// 4. Check for Windows Reserved Names (CON, NUL, etc.)
	// These names cause OS errors on Windows even with extensions (e.g. con.txt)
	if reservedNames.MatchString(strings.TrimSuffix(name, filepath.Ext(name))) {
		name = "_" + name
	}

	// 5. Trim Spaces and Dots from ends (Windows doesn't like trailing dots)
	name = strings.Trim(name, " .")

	// 6. Max Length Check (255 is standard max)
	if len(name) > 250 {
		ext := filepath.Ext(name)
		if len(ext) > 10 {
			ext = ""
		}
		name = name[:250-len(ext)] + ext
	}

	if name == "" {
		return "unnamed_file"
	}

	return name
}

// ensureExtension adds an extension based on Content-Type if missing
func ensureExtension(name, contentType string) string {
	if contentType == "" {
		return name
	}

	// If it already has a valid-looking extension, trust it
	ext := filepath.Ext(name)
	if ext != "" && len(ext) <= 6 {
		// Extra check: prevent .php or .html if content type is clearly video
		isMedia := strings.Contains(contentType, "video") || strings.Contains(contentType, "audio") || strings.Contains(contentType, "image")
		isScriptExt := strings.EqualFold(ext, ".php") || strings.EqualFold(ext, ".html") || strings.EqualFold(ext, ".asp") || strings.EqualFold(ext, ".jsp")

		if !(isMedia && isScriptExt) {
			return name
		}
	}

	// Clean content type (remove charset etc)
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = contentType
	}

	// Find extensions from OS MIME database
	extensions, _ := mime.ExtensionsByType(mediaType)
	if len(extensions) > 0 {
		return strings.TrimSuffix(name, ext) + extensions[0]
	}

	return name
}

// parseFilenameFromURL extracts filename from URL path
func parseFilenameFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	path := parsed.Path
	if path == "" || path == "/" {
		return ""
	}
	decodedPath, err := url.PathUnescape(path)
	if err != nil {
		decodedPath = path
	}
	return filepath.Base(decodedPath)
}

// ==========================================
// FORMATTING HELPERS
// ==========================================

func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// ==========================================
// PROGRESS LOGIC
// ==========================================

// shouldUpdateProgress uses both percentage AND time to prevent log spam
// while ensuring updates happen for slow connections.
func shouldUpdateProgress(total, downloaded int64, lastUpdatePercent int, lastUpdateTime time.Time) (bool, int) {
	if total <= 0 || downloaded <= 0 {
		return false, lastUpdatePercent
	}

	currentPercent := int((downloaded * 100) / total)

	// 1. Always update if finished
	if currentPercent == 100 && lastUpdatePercent != 100 {
		return true, 100
	}

	// 2. Time-based Throttling
	// Prevent updates faster than once every 2 seconds
	if time.Since(lastUpdateTime) < 2*time.Second {
		return false, lastUpdatePercent
	}

	// 3. Percentage-based Steps (Adaptive)
	// For huge files (>500MB), update every 1%
	// For small files, update every 5%
	step := 5
	if total > 500*1024*1024 {
		step = 1
	}

	if currentPercent >= lastUpdatePercent+step {
		return true, currentPercent
	}

	return false, lastUpdatePercent
}
