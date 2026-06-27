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
		"GET /api/state",
		func(writer http.ResponseWriter, request *http.Request) {
			writeJSON(writer, http.StatusOK, controller.State())
		},
	)
	mux.HandleFunc(
		"POST /api/start",
		func(writer http.ResponseWriter, request *http.Request) {
			if err := controller.Start(); err != nil {
				writeJSON(
					writer,
					http.StatusConflict,
					controller.State(),
				)
				return
			}
			writeJSON(writer, http.StatusOK, controller.State())
		},
	)
	mux.HandleFunc(
		"POST /api/stop",
		func(writer http.ResponseWriter, request *http.Request) {
			if err := controller.Stop(); err != nil {
				writeJSON(
					writer,
					http.StatusInternalServerError,
					controller.State(),
				)
				return
			}
			writeJSON(writer, http.StatusOK, controller.State())
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
  <title>LDGCC Worker</title>
  <style>
    :root { color-scheme: light; font-family: Inter, ui-sans-serif, system-ui, sans-serif; }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; background: #f4f5f7; color: #17202a; display: grid; place-items: center; }
    main { width: min(440px, calc(100vw - 32px)); background: #fff; border: 1px solid #d8dde3; border-radius: 8px; box-shadow: 0 12px 32px rgba(23,32,42,.08); }
    header { padding: 22px 24px 18px; border-bottom: 1px solid #e7eaee; }
    h1 { margin: 0; font-size: 20px; font-weight: 700; letter-spacing: 0; }
    section { padding: 24px; }
    .state { display: flex; align-items: center; gap: 12px; min-height: 40px; }
    .indicator { width: 12px; height: 12px; border-radius: 50%; background: #8a949f; box-shadow: 0 0 0 4px #edf0f2; }
    .indicator.running { background: #16825d; box-shadow: 0 0 0 4px #dff4ec; }
    .label { font-size: 16px; font-weight: 650; }
    .detail { margin-top: 4px; color: #64707d; font-size: 13px; }
    .error { min-height: 20px; margin: 18px 0 0; color: #b42318; font-size: 13px; }
    button { width: 100%; min-height: 44px; margin-top: 18px; border: 0; border-radius: 6px; background: #1565c0; color: #fff; font: inherit; font-weight: 700; cursor: pointer; }
    button.stop { background: #b42318; }
    button:disabled { cursor: wait; opacity: .62; }
  </style>
</head>
<body>
  <main>
    <header><h1>LDGCC Worker</h1></header>
    <section>
      <div class="state">
        <span id="indicator" class="indicator"></span>
        <div>
          <div id="label" class="label">Stopped</div>
          <div id="detail" class="detail">Not discoverable</div>
        </div>
      </div>
      <p id="error" class="error"></p>
      <button id="action" type="button">Start Worker</button>
    </section>
  </main>
  <script>
    const action = document.querySelector("#action");
    const indicator = document.querySelector("#indicator");
    const label = document.querySelector("#label");
    const detail = document.querySelector("#detail");
    const error = document.querySelector("#error");
    let state = { running: false, paired: false };

    function render(next) {
      state = next;
      indicator.classList.toggle("running", state.running);
      label.textContent = state.running ? "Worker running" : "Stopped";
      detail.textContent = state.running
        ? (state.paired ? "Paired and discoverable" : "Discoverable on LAN")
        : "Not discoverable";
      error.textContent = state.error || "";
      action.textContent = state.running ? "Stop Worker" : "Start Worker";
      action.classList.toggle("stop", state.running);
      action.disabled = false;
    }

    async function refresh() {
      const response = await fetch("/api/state");
      render(await response.json());
    }

    action.addEventListener("click", async () => {
      action.disabled = true;
      const endpoint = state.running ? "/api/stop" : "/api/start";
      try {
        const response = await fetch(endpoint, { method: "POST" });
        render(await response.json());
      } catch (requestError) {
        error.textContent = requestError.message;
        action.disabled = false;
      }
    });

    refresh();
    setInterval(refresh, 2000);
  </script>
</body>
</html>`
