package console

import (
	"context"
	"embed"
	"html/template"
	"net/http"
	"time"

	"github.com/docker/docker/client"
)

//go:embed templates/*.html
var templateFS embed.FS

// Serve starts the console HTTP server on addr (e.g. ":8080") and blocks until
// ctx is cancelled. It creates a Docker client, starts the background poller,
// and registers HTTP handlers.
func Serve(ctx context.Context, addr string) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer cli.Close()

	snap := &Snapshot{}
	poller := NewPoller(cli, snap)

	go poller.Run(ctx)

	tmpl, err := template.New("").Funcs(template.FuncMap{
		"formatTime": func(t time.Time) string {
			if t.IsZero() {
				return "—"
			}
			return t.Format("2006-01-02 15:04")
		},
	}).ParseFS(templateFS, "templates/index.html")
	if err != nil {
		return err
	}

	mux := http.NewServeMux()

	// Health endpoint for Traefik active healthcheck.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Dashboard.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		envs, lastUpdated := snap.Read()
		data := struct {
			Envs        []EnvSnapshot
			LastUpdated time.Time
		}{
			Envs:        envs,
			LastUpdated: lastUpdated,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
			http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		}
	})

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		return err
	}
}
