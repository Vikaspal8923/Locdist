package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	httpServer *http.Server
	listener   net.Listener
	token      string
	shutdown   func()
}

func NewServer(port string, controller *Controller) (*Server, error) {
	return NewSecureServer("127.0.0.1", port, "", controller)
}

func NewSecureServer(host, port, token string, controller *Controller) (*Server, error) {
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return nil, fmt.Errorf("Master App must bind to loopback")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", serveApp)
	mux.HandleFunc("GET /health", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]any{"status": "ok", "version": Version})
	})
	mux.HandleFunc("GET /state", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, http.StatusOK, controller.State())
	})
	mux.HandleFunc("POST /discovery/start", func(writer http.ResponseWriter, request *http.Request) {
		controller.Events().Publish("discovery.started", nil)
		writeJSON(writer, http.StatusAccepted, map[string]string{"status": "started"})
	})
	mux.HandleFunc("GET /workers/discovered", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, http.StatusOK, controller.Workers())
	})
	mux.HandleFunc("POST /workers/{instance}/pair", func(writer http.ResponseWriter, request *http.Request) {
		if err := controller.Pair(request.PathValue("instance")); err != nil {
			writeError(writer, http.StatusConflict, err)
			return
		}
		writeJSON(writer, http.StatusAccepted, map[string]string{"status": "pending"})
	})
	mux.HandleFunc("POST /jobs/prepare", func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			ProjectRoot string `json:"project_root"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body.ProjectRoot == "" {
			writeError(writer, http.StatusBadRequest, fmt.Errorf("project_root is required"))
			return
		}
		go func() {
			if err := controller.Prepare(context.Background(), body.ProjectRoot); err != nil {
				controller.Events().Publish("command.rejected", map[string]string{"command": "prepare", "error": err.Error()})
			}
		}()
		writeJSON(writer, http.StatusAccepted, map[string]string{"status": "preparing"})
	})
	mux.HandleFunc("POST /jobs/setup", func(writer http.ResponseWriter, request *http.Request) {
		go func() {
			if err := controller.Setup(context.Background(), false); err != nil {
				controller.Events().Publish("command.rejected", map[string]string{"command": "setup", "error": err.Error()})
			}
		}()
		writeJSON(writer, http.StatusAccepted, map[string]string{"status": "setting_up"})
	})
	mux.HandleFunc("POST /jobs/setup/retry", func(writer http.ResponseWriter, request *http.Request) {
		go func() {
			if err := controller.Setup(context.Background(), true); err != nil {
				controller.Events().Publish("command.rejected", map[string]string{"command": "setup_retry", "error": err.Error()})
			}
		}()
		writeJSON(writer, http.StatusAccepted, map[string]string{"status": "retrying"})
	})
	mux.HandleFunc("POST /jobs/start", func(writer http.ResponseWriter, request *http.Request) {
		if err := controller.Start(request.Context()); err != nil {
			writeError(writer, http.StatusConflict, err)
			return
		}
		writeJSON(writer, http.StatusAccepted, map[string]string{"status": "started"})
	})
	mux.HandleFunc("POST /jobs/stop", func(writer http.ResponseWriter, request *http.Request) {
		if err := controller.Stop(); err != nil {
			writeError(writer, http.StatusConflict, err)
			return
		}
		writeJSON(writer, http.StatusAccepted, map[string]string{"status": "stopping"})
	})
	mux.HandleFunc("GET /jobs/current", func(writer http.ResponseWriter, request *http.Request) {
		state := controller.State()
		if state.Job == nil {
			writeError(writer, http.StatusNotFound, fmt.Errorf("no active job"))
			return
		}
		writeJSON(writer, http.StatusOK, state.Job)
	})
	mux.HandleFunc("GET /jobs/last-summary", func(writer http.ResponseWriter, request *http.Request) {
		state := controller.State()
		if state.LastSummary == nil {
			writeError(writer, http.StatusNotFound, fmt.Errorf("no completed job summary"))
			return
		}
		writeJSON(writer, http.StatusOK, state.LastSummary)
	})
	mux.HandleFunc("GET /results/{job_id}", func(writer http.ResponseWriter, request *http.Request) {
		path, err := controller.ResultPath(request.PathValue("job_id"))
		if err != nil {
			writeError(writer, http.StatusNotFound, err)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]string{"job_id": request.PathValue("job_id"), "path": path})
	})
	mux.HandleFunc("GET /events", func(writer http.ResponseWriter, request *http.Request) { serveEvents(writer, request, controller) })
	server := &Server{token: token}
	mux.HandleFunc("POST /shutdown", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, http.StatusAccepted, map[string]string{"status": "shutting_down"})
		if server.shutdown != nil {
			go server.shutdown()
		}
	})
	mux.HandleFunc(
		"GET /api/workers",
		func(writer http.ResponseWriter, request *http.Request) {
			writeJSON(writer, http.StatusOK, controller.Workers())
		},
	)
	mux.HandleFunc(
		"POST /api/pair",
		func(writer http.ResponseWriter, request *http.Request) {
			var body struct {
				Instance string `json:"instance"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				http.Error(writer, "invalid request", http.StatusBadRequest)
				return
			}
			if err := controller.Pair(body.Instance); err != nil {
				http.Error(writer, err.Error(), http.StatusConflict)
				return
			}
			writeJSON(
				writer,
				http.StatusAccepted,
				map[string]string{"status": "PENDING"},
			)
		},
	)

	server.httpServer = &http.Server{
		Handler:           server.authenticate(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	server.listener = listener
	return server, nil
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if s.token != "" && request.Header.Get("Authorization") != "Bearer "+s.token && request.Header.Get("X-LDGCC-Token") != s.token {
			writeError(writer, http.StatusUnauthorized, fmt.Errorf("invalid session token"))
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (s *Server) SetShutdown(callback func()) { s.shutdown = callback }

func serveEvents(writer http.ResponseWriter, request *http.Request, controller *Controller) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeError(writer, http.StatusInternalServerError, fmt.Errorf("streaming is unavailable"))
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	events, unsubscribe := controller.Events().Subscribe()
	defer unsubscribe()
	_, _ = fmt.Fprintf(writer, "event: state\ndata: %s\n\n", eventJSON(Event{Type: "state", Time: time.Now(), Data: controller.State()}))
	flusher.Flush()
	for {
		select {
		case <-request.Context().Done():
			return
		case event := <-events:
			_, _ = fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", strings.ReplaceAll(event.Type, "\n", ""), eventJSON(event))
			flusher.Flush()
		}
	}
}

func writeError(writer http.ResponseWriter, status int, err error) {
	writeJSON(writer, status, map[string]string{"error": err.Error()})
}

func (s *Server) Start() error {
	err := s.httpServer.Serve(s.listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (s *Server) Stop() error {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) Address() string {
	return fmt.Sprintf("http://%s", s.listener.Addr())
}

func writeJSON(
	writer http.ResponseWriter,
	statusCode int,
	value any,
) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(value)
}

func serveApp(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = writer.Write([]byte(appHTML))
}

const appHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>LDGCC Master</title>
  <style>
    :root { font-family: Inter, ui-sans-serif, system-ui, sans-serif; color: #17202a; }
    * { box-sizing: border-box; }
    body { margin: 0; background: #f4f5f7; min-height: 100vh; }
    header { background: #fff; border-bottom: 1px solid #d8dde3; padding: 18px 28px; }
    h1 { margin: 0; font-size: 20px; letter-spacing: 0; }
    main { width: min(920px, calc(100vw - 32px)); margin: 32px auto; }
    h2 { font-size: 15px; letter-spacing: 0; margin: 0 0 12px; }
    .empty { color: #66717d; padding: 28px 0; }
    .worker { display: grid; grid-template-columns: minmax(0,1fr) minmax(160px,.6fr) 120px; gap: 18px; align-items: center; background: #fff; border: 1px solid #d8dde3; border-radius: 7px; padding: 16px; margin-bottom: 10px; }
    .name { font-weight: 700; }
    .meta { color: #66717d; font-size: 13px; margin-top: 4px; }
    .status { font-size: 13px; text-transform: lowercase; }
    button { min-height: 38px; border: 0; border-radius: 6px; background: #1565c0; color: #fff; font: inherit; font-weight: 700; cursor: pointer; }
    button:disabled { opacity: .55; cursor: wait; }
    .error { color: #b42318; }
  </style>
</head>
<body>
  <header><h1>LDGCC Master</h1></header>
  <main>
    <h2>Discovered Workers</h2>
    <div id="workers"></div>
  </main>
  <script>
    const workers = document.querySelector("#workers");

    async function pair(instance) {
      await fetch("/api/pair", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ instance })
      });
      refresh();
    }

    async function refresh() {
      const response = await fetch("/api/workers");
      const items = await response.json();
      if (!items.length) {
        workers.innerHTML = '<div class="empty">No Workers discovered</div>';
        return;
      }
      workers.replaceChildren(...items.map(item => {
        const row = document.createElement("div");
        row.className = "worker";
        const identity = document.createElement("div");
        identity.innerHTML = '<div class="name"></div><div class="meta"></div>';
        identity.querySelector(".name").textContent = item.instance;
        identity.querySelector(".meta").textContent = item.address;
        const status = document.createElement("div");
        status.className = item.error ? "status error" : "status";
        status.textContent = item.error || item.request_status || item.pairing_status;
        const button = document.createElement("button");
        button.textContent = "Connect";
        button.disabled = item.pairing_status !== "unpaired" || item.request_status === "PENDING";
        button.addEventListener("click", () => pair(item.instance));
        row.append(identity, status, button);
        return row;
      }));
    }

    refresh();
    setInterval(refresh, 2000);
  </script>
</body>
</html>`
