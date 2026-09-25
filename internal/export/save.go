package export

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/gammons/slk/internal/ui/messages"
)

// SaveThread writes the thread as Markdown into ExportDir and returns
// the new file's path. The name is built from channelName and the time.
func SaveThread(parent messages.MessageItem, replies []messages.MessageItem, userNames, channelNames map[string]string, channelName string) (string, error) {
	content := ThreadToMarkdown(parent, replies, userNames, channelNames)

	dir, err := ExportDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	filename := fmt.Sprintf("slk-thread-%s-%s.md", sanitizeForFilename(channelName), time.Now().Format("2006-01-02-150405"))
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func sanitizeForFilename(s string) string {
	var b strings.Builder
	prev := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(r)
			prev = false
		} else if !prev {
			b.WriteByte('-')
			prev = true
		}
	}
	result := strings.Trim(b.String(), "-")
	if result == "" {
		return "unknown"
	}
	return result
}
