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
// relay network under the given app name, and registers GET /relays so the
// web UI can show which relays are currently connected.
func runPortal(mux *http.ServeMux, name string, hide bool, relayURLs []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	exposure, err := sdk.Expose(ctx, sdk.ExposeConfig{
		RelayURLs:       relayURLs,
		Discovery:       true,
		MaxActiveRelays: 3,
		Identity:        types.Identity{Name: name},
		IdentityPath:    "identity.json",
		Metadata: types.LeaseMetadata{
			Description: "URL shortener",
			Tags:        []string{"url-shortener", "tools", "agent"},
			Thumbnail:   "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(thumbnailJPEG),
			Hide:        hide,
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

	// relays only, not the full snapshot — that repeats the thumbnail data URI
	mux.HandleFunc("/relays", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{"relays": exposure.Snapshot().Relays})
	})

	log.Println("portal tunnel starting; watch stderr for relay connection logs")
	return exposure.RunHTTP(ctx, mux, ":8000")
}
