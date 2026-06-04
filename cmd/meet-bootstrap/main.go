// One-time helper that produces a Google Meet refresh token to paste
// into the server's .env (as MEET_REFRESH_TOKEN). Run this once per
// environment, on whichever machine has a browser, after you've:
//
//   1. Created the Workspace user the bot will run as (e.g.
//      meet-bot@yourdomain).
//   2. Enabled the Google Meet REST API in your Google Cloud project.
//   3. Created an OAuth client (type: Web application) in that
//      project, with `http://localhost:8765/callback` listed under
//      Authorized redirect URIs.
//   4. Configured the OAuth consent screen with the
//      `https://www.googleapis.com/auth/meetings.space.created` scope.
//
// Usage:
//
//   MEET_OAUTH_CLIENT_ID=<id> MEET_OAUTH_CLIENT_SECRET=<secret> \
//       go run ./cmd/meet-bootstrap
//
// The script:
//   1. Prints a URL to open in your browser
//   2. Spins up a tiny local web server on :8765 to catch the
//      OAuth redirect (so you don't have to paste the code by hand)
//   3. Exchanges the returned code for a refresh token
//   4. Prints the refresh token — paste it into .env as
//      MEET_REFRESH_TOKEN
//
// The refresh token survives password changes on the Workspace user
// account (it's tied to the OAuth grant, not the password). Revoke it
// from console.cloud.google.com if you ever need to rotate.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hngprojects/personal-trainer-be/pkg/googlemeet"
)

const (
	// Port the loopback server listens on. Has to match the
	// "Authorized redirect URIs" you configured in Google Cloud
	// Console. Picking 8765 to avoid common dev-server collisions on
	// 8080/3000.
	loopbackPort = "8765"
	redirectURI  = "http://localhost:" + loopbackPort + "/callback"
)

func main() {
	clientID := os.Getenv("MEET_OAUTH_CLIENT_ID")
	clientSecret := os.Getenv("MEET_OAUTH_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		log.Fatal("set MEET_OAUTH_CLIENT_ID and MEET_OAUTH_CLIENT_SECRET in the environment before running this script (get them from console.cloud.google.com → APIs & Services → Credentials)")
	}

	// Make sure the loopback port is free before we send the user to
	// Google — otherwise they'd consent successfully but the redirect
	// would land on someone else's local service.
	ln, err := net.Listen("tcp", ":"+loopbackPort)
	if err != nil {
		log.Fatalf("can't bind localhost:%s — is another process using it? %v", loopbackPort, err)
	}

	// Channel the HTTP handler sends the auth code through. Buffered
	// so the handler can write + return even if main hasn't reached
	// the read yet.
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			errCh <- fmt.Errorf("oauth error from Google: %s — %s", errMsg, r.URL.Query().Get("error_description"))
			_, _ = fmt.Fprintln(w, "OAuth flow failed. Check the terminal for details. You can close this tab.")
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- fmt.Errorf("redirect URL contained no `code` parameter — got %s", r.URL.String())
			return
		}
		_, _ = fmt.Fprintln(w, "Success — refresh token printed in the terminal. You can close this tab.")
		codeCh <- code
	})

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	defer func() { _ = srv.Shutdown(context.Background()) }()

	authURL := googlemeet.AuthorizationURL(clientID, redirectURI)
	fmt.Println()
	fmt.Println("Open this URL in your browser and consent as the meet-bot Workspace user:")
	fmt.Println()
	fmt.Println("  ", authURL)
	fmt.Println()
	fmt.Println("Waiting for the OAuth redirect on", redirectURI, "...")
	fmt.Println("(Ctrl+C to abort.)")

	// Cancel cleanly on Ctrl+C — otherwise an aborted attempt leaves
	// the loopback port held until the OS reaps it.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		log.Fatalf("bootstrap failed: %v", err)
	case <-sigCh:
		log.Fatal("aborted by user")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	refreshToken, err := googlemeet.ExchangeCode(ctx, clientID, clientSecret, code, redirectURI)
	if err != nil {
		log.Fatalf("code exchange failed: %v", err)
	}

	fmt.Println()
	fmt.Println("✅  Refresh token received. Paste this into the server's .env:")
	fmt.Println()
	fmt.Println("MEET_REFRESH_TOKEN=" + refreshToken)
	fmt.Println()
	fmt.Println("Then set MEET_ENABLED=true and restart the server. Test by booking")
	fmt.Println("a session with session_platform=google_meet — the booking row should")
	fmt.Println("come back with a `https://meet.google.com/…` link in zoom_meeting_link.")
}
