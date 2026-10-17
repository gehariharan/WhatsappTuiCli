package tui

import (
	"strings"
	"testing"
)

func TestRichAttachmentDetails(t *testing.T) {
	got := mediaDetails("document", `{"mime":"application/pdf","filename":"report.pdf","size":1048576,"pages":3,"download_state":"downloaded","relative_path":"1/chat/file.pdf"}`)
	for _, want := range []string{"File: report.pdf", "MIME: application/pdf", "1.00 MiB", "Pages: 3", "Download: downloaded", "File under media/: 1/chat/file.pdf"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	got = mediaDetails("audio", `{"voice_note":true,"duration_seconds":65}`)
	if !strings.Contains(got, "Voice note") || !strings.Contains(got, "1:05") {
		t.Fatal(got)
	}
	got = mediaDetails("reaction", `{"target_message_id":"target","removed":true,"emoji":""}`)
	if !strings.Contains(got, "Reaction removed") || !strings.Contains(got, "Target: target") {
		t.Fatal(got)
	}
	got = mediaDetails("document", `{"filename":"\u001b[31munsafe\nname"}`)
	if strings.ContainsAny(got, "\x1b\r") || strings.Count(got, "\n") > 0 {
		t.Fatal("terminal controls survived", got)
	}
}
