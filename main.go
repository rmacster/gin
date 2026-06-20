package main

import (
	"log"
	"net/http"
	"os"

	"gin-server/database"
	"gin-server/handlers"

	"github.com/gorilla/mux"
)

func main() {
	dbPath := envOr("GIN_DB", "gin.db")
	addr := envOr("GIN_ADDR", ":8090")

	if err := database.Initialize(dbPath); err != nil {
		log.Fatalf("database init failed: %v", err)
	}

	srv := handlers.NewServer()
	srv.RestoreGames() // reload active games so they survive restarts
	r := mux.NewRouter()
	api := r.PathPrefix("/api").Subrouter()

	// Public.
	api.HandleFunc("/register", handlers.Register).Methods("POST")

	// Authenticated (any approved user or admin).
	api.HandleFunc("/me", handlers.BasicAuth(handlers.Me)).Methods("GET")

	// Player-only.
	api.HandleFunc("/players", handlers.PlayerOnly(handlers.ListPlayers)).Methods("GET")
	api.HandleFunc("/groups", handlers.PlayerOnly(handlers.ListGroups)).Methods("GET")
	api.HandleFunc("/groups", handlers.PlayerOnly(handlers.CreateGroup)).Methods("POST")
	api.HandleFunc("/groups/{id}/members", handlers.PlayerOnly(handlers.AddGroupMember)).Methods("POST")
	api.HandleFunc("/groups/{id}/invite", handlers.PlayerOnly(srv.InviteGroup)).Methods("POST")
	api.HandleFunc("/groups/{id}", handlers.PlayerOnly(handlers.DeleteGroup)).Methods("DELETE")
	api.HandleFunc("/games", handlers.PlayerOnly(srv.ListGames)).Methods("GET")
	api.HandleFunc("/games", handlers.PlayerOnly(srv.CreateGame)).Methods("POST")
	api.HandleFunc("/games/{id}", handlers.PlayerOnly(srv.ClearGame)).Methods("DELETE")
	api.HandleFunc("/games/{id}/decline", handlers.PlayerOnly(srv.DeclineInvite)).Methods("POST")

	// Admin-only.
	api.HandleFunc("/admin/pending", handlers.AdminOnly(handlers.AdminListPending)).Methods("GET")
	api.HandleFunc("/admin/users", handlers.AdminOnly(handlers.AdminListUsers)).Methods("GET")
	api.HandleFunc("/admin/users", handlers.AdminOnly(handlers.AdminCreateUser)).Methods("POST")
	api.HandleFunc("/admin/users/{id}/approve", handlers.AdminOnly(handlers.AdminApprove)).Methods("POST")
	api.HandleFunc("/admin/users/{id}/reject", handlers.AdminOnly(handlers.AdminReject)).Methods("POST")
	api.HandleFunc("/admin/users/{id}", handlers.AdminOnly(handlers.AdminDeleteUser)).Methods("DELETE")

	// Real-time gameplay (auth handled inside via Basic Auth or query params).
	r.HandleFunc("/ws", srv.ServeWS)
	// Lobby presence + invite delivery (same auth scheme as /ws).
	r.HandleFunc("/lobby", srv.ServeLobby)

	// Static frontend. no-cache makes browsers revalidate, so updated assets
	// (app.js/style.css) are picked up without a manual hard-refresh.
	r.PathPrefix("/").Handler(noCache(http.FileServer(http.Dir("static"))))

	certFile := os.Getenv("TLS_CERT")
	keyFile := os.Getenv("TLS_KEY")
	if certFile != "" && keyFile != "" {
		go serveHTTPRedirect() // :80 -> ACME challenges + redirect to HTTPS
		log.Printf("Gin Rummy server listening on :443 (TLS) (db: %s)", dbPath)
		if err := http.ListenAndServeTLS(":443", certFile, keyFile, r); err != nil {
			log.Fatal(err)
		}
		return
	}

	log.Printf("Gin Rummy server listening on %s (db: %s)", addr, dbPath)
	log.Printf("Default admin login: admin / gin2024")
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatal(err)
	}
}

// serveHTTPRedirect runs the plain-HTTP :80 listener used in production: it
// serves Let's Encrypt webroot ACME challenges and redirects everything else to
// HTTPS, mirroring the CharmToolWeb deployment.
func serveHTTPRedirect() {
	webroot := envOr("ACME_WEBROOT", "/var/www/letsencrypt")
	mux := http.NewServeMux()
	mux.Handle("/.well-known/acme-challenge/", http.FileServer(http.Dir(webroot)))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://"+r.Host+r.URL.RequestURI(), http.StatusMovedPermanently)
	})
	if err := http.ListenAndServe(":80", mux); err != nil {
		log.Printf("http redirect listener: %v", err)
	}
}

// noCache asks browsers to revalidate cached static assets before using them, so
// updated app.js/style.css are served after a deploy without a hard-refresh.
func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		h.ServeHTTP(w, r)
	})
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
