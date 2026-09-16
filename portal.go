// Embeds Portal's Go SDK so `go run . -portal` is both the local server and
// the public tunnel in one process, with live relay status at GET /relays.
package main

import (
	"context"
	_ "embed"
	"encoding/base64"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gosuda/portal-tunnel/v2/sdk"
	"github.com/gosuda/portal-tunnel/v2/types"
)

//go:embed potly-thumbnail.jpg
var thumbnailJPEG []byte

// runPortal exposes mux both locally on :8000 and publicly through Portal's
// relay network, and registers GET /relays so the web UI can show which
// relays are currently connected.
func runPortal(mux *http.ServeMux) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	exposure, err := sdk.Expose(ctx, sdk.ExposeConfig{
		Discovery:       true,
		MaxActiveRelays: 3,
		Identity:        types.Identity{Name: "potly"},
		IdentityPath:    "identity.json",
		Metadata: types.LeaseMetadata{
			Description: "URL shortener",
			Tags:        []string{"url-shortener", "tools", "agent"},
			Thumbnail:   "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(thumbnailJPEG),
		},
	})
	if err != nil {
		return err
	}
	defer exposure.Close()

	activeRelayBaseURLs = func() []string {
		relays := exposure.Snapshot().Relays
		out := make([]string, 0, len(relays))
		for _, relay := range relays {
			if relay.PublicURL != "" {
				out = append(out, relay.PublicURL)
			}
		}
		return out
	}

	// Only the relay list, not the whole snapshot (which repeats the
	// data-URI thumbnail) since the web UI polls this every few seconds.
	mux.HandleFunc("/relays", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{"relays": exposure.Snapshot().Relays})
	})

	log.Println("portal tunnel starting; watch stderr for relay connection logs")
	return exposure.RunHTTP(ctx, mux, ":8000")
}
