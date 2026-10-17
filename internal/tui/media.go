package tui

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Attachment metadata is untrusted message content. Sanitize each displayed
// value and never open a file or URL as a side effect of browsing the archive.
func mediaDetails(kind, raw string) string {
	var data struct {
		MIME       string `json:"mime"`
		Filename   string `json:"filename"`
		Title      string `json:"title"`
		Size       uint64 `json:"size"`
		Width      uint32 `json:"width"`
		Height     uint32 `json:"height"`
		Duration   uint32 `json:"duration_seconds"`
		Pages      uint32 `json:"pages"`
		VoiceNote  bool   `json:"voice_note"`
		Animated   bool   `json:"animated"`
		Lottie     bool   `json:"lottie"`
		State      string `json:"download_state"`
		Path       string `json:"relative_path"`
		LegacyPath string `json:"local_path"`
		Target     string `json:"target_message_id"`
		Emoji      string `json:"emoji"`
		Removed    bool   `json:"removed"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return "Attachment metadata unavailable"
	}
	lines := []string{}
	if kind == "reaction" {
		action := "Reaction: " + oneLine(data.Emoji)
		if data.Removed {
			action = "Reaction removed"
		}
		return action + "\nTarget: " + oneLine(data.Target)
	}
	for _, field := range []struct{ label, value string }{{"File", data.Filename}, {"Title", data.Title}, {"MIME", data.MIME}} {
		if field.value != "" {
			lines = append(lines, field.label+": "+oneLine(field.value))
		}
	}
	if data.Size > 0 {
		lines = append(lines, fmt.Sprintf("Size: %d bytes (%.2f MiB)", data.Size, float64(data.Size)/(1<<20)))
	}
	if data.Width > 0 || data.Height > 0 {
		lines = append(lines, fmt.Sprintf("Dimensions: %d × %d", data.Width, data.Height))
	}
	if data.Duration > 0 {
		lines = append(lines, fmt.Sprintf("Duration: %d:%02d", data.Duration/60, data.Duration%60))
	}
	if data.Pages > 0 {
		lines = append(lines, fmt.Sprintf("Pages: %d", data.Pages))
	}
	if data.VoiceNote {
		lines = append(lines, "Voice note")
	}
	if data.Animated {
		lines = append(lines, "Animated")
	}
	if data.Lottie {
		lines = append(lines, "Lottie sticker (metadata only)")
	}
	if data.State != "" {
		lines = append(lines, "Download: "+oneLine(data.State))
	}
	if data.Path != "" {
		lines = append(lines, "File under media/: "+oneLine(data.Path))
	} else if data.LegacyPath != "" {
		lines = append(lines, "Local file: "+oneLine(data.LegacyPath))
	}
	return strings.Join(lines, "\n")
}
