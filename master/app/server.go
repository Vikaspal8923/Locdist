package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

type Server struct {
	httpServer *http.Server
	listener   net.Listener
}

func NewServer(port string, controller *Controller) (*Server, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", serveApp)
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

	return &Server{
		httpServer: &http.Server{
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		},
		listener: listener,
	}, nil
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
