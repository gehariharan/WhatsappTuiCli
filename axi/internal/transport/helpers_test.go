package transport

import (
	"net/http"
	"os"
)

func readIfExists(path string) ([]byte, error) { return os.ReadFile(path) }

func blockHandler(done <-chan struct{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-r.Context().Done():
		}
	})
}
