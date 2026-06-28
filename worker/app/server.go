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
		"POST /api/config",
		func(writer http.ResponseWriter, request *http.Request) {
			var update ConfigUpdate
			if err := json.NewDecoder(request.Body).Decode(&update); err != nil {
				writeJSON(writer, http.StatusBadRequest, controller.State())
				return
			}
			if err := controller.UpdateConfig(update); err != nil {
				writeJSON(writer, http.StatusConflict, controller.State())
				return
			}
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
	mux.HandleFunc(
		"POST /api/pairing/accept",
		actionHandler(controller.AcceptPairing, controller),
	)
	mux.HandleFunc(
		"POST /api/pairing/reject",
		actionHandler(controller.RejectPairing, controller),
	)
	mux.HandleFunc(
		"POST /api/pairing/reset",
		actionHandler(controller.ResetPairing, controller),
	)

	return &Server{
		httpServer: &http.Server{
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		},
		listener: listener,
	}, nil
}

func actionHandler(
	action func() error,
	controller *Controller,
) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if err := action(); err != nil {
			writeJSON(
				writer,
				http.StatusConflict,
				controller.State(),
			)
			return
		}
		writeJSON(writer, http.StatusOK, controller.State())
	}
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
    :root { color-scheme: light; font-family: "Aptos", "Segoe UI", ui-sans-serif, system-ui, sans-serif; background: #eef2f3; color: #17202a; }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; background: linear-gradient(135deg, #eef2f3 0%, #f8faf6 44%, #eef6fb 100%); }
    main { width: min(960px, calc(100vw - 32px)); margin: 32px auto; }
    header { display: flex; justify-content: space-between; gap: 18px; align-items: center; margin-bottom: 18px; }
    h1 { margin: 0; font-size: 28px; font-weight: 760; letter-spacing: 0; }
    .subtitle { margin-top: 4px; color: #607080; font-size: 14px; }
    .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; align-items: start; }
    .panel { background: rgba(255,255,255,.92); border: 1px solid #d7dee5; border-radius: 8px; box-shadow: 0 12px 34px rgba(36,48,58,.08); padding: 20px; }
    .wide { grid-column: 1 / -1; }
    .state { display: flex; align-items: center; gap: 14px; min-height: 50px; }
    .indicator { width: 14px; height: 14px; border-radius: 50%; background: #8a949f; box-shadow: 0 0 0 5px #edf0f2; flex: 0 0 auto; }
    .indicator.running { background: #16825d; box-shadow: 0 0 0 5px #dff4ec; }
    .indicator.pending { background: #d97706; box-shadow: 0 0 0 5px #fff0d8; }
    .label { font-size: 18px; font-weight: 720; text-transform: capitalize; }
    .detail { margin-top: 4px; color: #64707d; font-size: 13px; overflow-wrap: anywhere; }
    .facts { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; margin-top: 18px; }
    .fact { border: 1px solid #e3e8ed; border-radius: 7px; padding: 10px 12px; background: #fbfcfd; min-height: 58px; }
    .fact span, label { display: block; color: #6a7581; font-size: 12px; font-weight: 650; margin-bottom: 5px; }
    .fact strong { display: block; font-size: 14px; overflow-wrap: anywhere; }
    .request { display: none; margin-top: 18px; padding: 16px; border: 1px solid #d8dde3; border-radius: 7px; background: #f8fafb; }
    .request.visible { display: block; }
    .request strong { display: block; font-size: 14px; }
    .request-actions { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
    .error { min-height: 20px; margin: 18px 0 0; color: #b42318; font-size: 13px; }
    .actions { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; margin-top: 18px; }
    button { width: 100%; min-height: 42px; border: 0; border-radius: 6px; background: #1565c0; color: #fff; font: inherit; font-weight: 700; cursor: pointer; }
    button.stop { background: #b42318; }
    button.secondary { background: #fff; color: #344054; border: 1px solid #cfd5dc; }
    button.danger { background: #fff; color: #b42318; border: 1px solid #e5aaa5; }
    button:disabled { cursor: wait; opacity: .62; }
    form { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
    input { width: 100%; min-height: 38px; border: 1px solid #ccd5dd; border-radius: 6px; padding: 8px 10px; font: inherit; }
    input:disabled { background: #f3f5f7; color: #7a8490; }
    .form-actions { grid-column: 1 / -1; display: flex; justify-content: flex-end; }
    .form-actions button { width: auto; min-width: 120px; padding: 0 18px; }
    @media (max-width: 760px) {
      main { margin: 18px auto; }
      header, .grid, form, .facts, .actions { grid-template-columns: 1fr; display: grid; }
      .form-actions { justify-content: stretch; }
      .form-actions button { width: 100%; }
    }
  </style>
</head>
<body>
  <main>
    <header>
      <div>
        <h1>LDGCC Worker</h1>
        <div id="worker-name" class="subtitle">Worker laptop</div>
      </div>
      <button id="refresh" class="secondary" type="button">Refresh</button>
    </header>
    <div class="grid">
    <section class="panel">
      <div class="state">
        <span id="indicator" class="indicator"></span>
        <div>
          <div id="label" class="label">Stopped</div>
          <div id="detail" class="detail">Not discoverable</div>
        </div>
      </div>
      <div class="facts">
        <div class="fact"><span>Worker gRPC</span><strong id="grpc"></strong></div>
        <div class="fact"><span>App</span><strong id="app"></strong></div>
        <div class="fact"><span>Workspace</span><strong id="workspace"></strong></div>
        <div class="fact"><span>Master</span><strong id="master"></strong></div>
      </div>
      <div id="request" class="request">
        <strong id="request-title"></strong>
        <div class="request-actions">
          <button id="reject" class="secondary" type="button">Reject</button>
          <button id="accept" type="button">Accept</button>
        </div>
      </div>
      <p id="error" class="error"></p>
      <div class="actions">
        <button id="action" type="button">Start Worker</button>
        <button id="reset" class="danger" type="button" hidden>Reset Previous Connection</button>
      </div>
    </section>
    <section class="panel">
      <form id="settings">
        <label>Worker name<input id="name" name="worker_name" autocomplete="off"></label>
        <label>Host<input id="host" name="host" autocomplete="off"></label>
        <label>Worker gRPC port<input id="grpc-port" name="grpc_port" inputmode="numeric"></label>
        <label>Workspace root<input id="workspace-root" name="workspace_root" autocomplete="off"></label>
        <div class="form-actions"><button id="save" type="submit">Save Settings</button></div>
      </form>
    </section>
    </div>
  </main>
  <script>
    const action = document.querySelector("#action");
    const indicator = document.querySelector("#indicator");
    const label = document.querySelector("#label");
    const detail = document.querySelector("#detail");
    const error = document.querySelector("#error");
    const request = document.querySelector("#request");
    const requestTitle = document.querySelector("#request-title");
    const accept = document.querySelector("#accept");
    const reject = document.querySelector("#reject");
    const reset = document.querySelector("#reset");
    const refreshButton = document.querySelector("#refresh");
    const settings = document.querySelector("#settings");
    const save = document.querySelector("#save");
    const fields = {
      name: document.querySelector("#name"),
      host: document.querySelector("#host"),
      grpcPort: document.querySelector("#grpc-port"),
      workspaceRoot: document.querySelector("#workspace-root"),
    };
    let state = { running: false, connection: "UNPAIRED" };

    function render(next) {
      state = next;
      const pending = Boolean(state.pending_pairing);
      indicator.classList.toggle("running", state.running && !pending);
      indicator.classList.toggle("pending", pending);
      document.querySelector("#worker-name").textContent = state.config.worker_name || "Worker laptop";
      label.textContent = state.status || "stopped";
      detail.textContent = state.running
        ? "Discoverable on LAN as " + state.config.worker_name
        : "Not discoverable";
      document.querySelector("#grpc").textContent = state.config.host + ":" + state.config.grpc_port;
      document.querySelector("#app").textContent = "127.0.0.1:" + state.config.app_port;
      document.querySelector("#workspace").textContent = state.config.workspace_root;
      document.querySelector("#master").textContent = state.paired_master || "none";
      error.textContent = state.error || "";
      action.textContent = state.running ? "Stop Worker" : "Start Worker";
      action.classList.toggle("stop", state.running);
      action.disabled = false;
      request.classList.toggle("visible", pending);
      requestTitle.textContent = pending
        ? state.pending_pairing.master_name + " wants to connect"
        : "";
      reset.hidden = !state.paired_master || pending;
      fields.name.value = state.config.worker_name || "";
      fields.host.value = state.config.host || "";
      fields.grpcPort.value = state.config.grpc_port || "";
      fields.workspaceRoot.value = state.config.workspace_root || "";
      for (const input of Object.values(fields)) input.disabled = state.running;
      save.disabled = state.running;
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

    async function pairingAction(endpoint) {
      accept.disabled = true;
      reject.disabled = true;
      const response = await fetch(endpoint, { method: "POST" });
      render(await response.json());
      accept.disabled = false;
      reject.disabled = false;
    }

    accept.addEventListener("click", () => pairingAction("/api/pairing/accept"));
    reject.addEventListener("click", () => pairingAction("/api/pairing/reject"));
    refreshButton.addEventListener("click", refresh);
    settings.addEventListener("submit", async (event) => {
      event.preventDefault();
      save.disabled = true;
      const response = await fetch("/api/config", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          worker_name: fields.name.value,
          host: fields.host.value,
          grpc_port: fields.grpcPort.value,
          workspace_root: fields.workspaceRoot.value,
        }),
      });
      render(await response.json());
    });
    reset.addEventListener("click", async () => {
      if (!confirm("Remove the saved Master connection?")) return;
      const response = await fetch("/api/pairing/reset", { method: "POST" });
      render(await response.json());
    });

    refresh();
    setInterval(refresh, 2000);
  </script>
</body>
</html>`
