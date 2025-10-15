package openaiserver

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/kubeai-project/kubeai/internal/modelproxy"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Handler struct {
	ModelProxy *modelproxy.Handler
	K8sClient  client.Client
	http.Handler
}

func NewHandler(k8sClient client.Client, modelProxy *modelproxy.Handler) *Handler {
	h := &Handler{
		K8sClient: k8sClient,
	}

	mux := http.NewServeMux()
	// handle is a replacement for mux.Handle
	// which enriches the handler's HTTP instrumentation with the pattern as the http.route.
	handle := func(pattern string, routeHandler http.Handler) {
		// Configure the "http.route" for the HTTP instrumentation.
		mux.Handle(pattern, otelhttp.WithRouteTag(pattern, routeHandler))
	}

	// NOTE: Proxying all paths to backend engines is a security risk.
	// Make sure to only proxy paths that are safe to expose to the public.
	// Example: vLLM supports loading arbitrary model adapaters via the API
	// at `/v1/load_lora_adapter`.

	handle("/openai/v1/chat/completions", http.StripPrefix("/openai", modelProxy))
	handle("/openai/v1/completions", http.StripPrefix("/openai", modelProxy))
	handle("/openai/v1/embeddings", http.StripPrefix("/openai", modelProxy))
	handle("/openai/v1/rerank", http.StripPrefix("/openai", modelProxy))
	handle("/openai/v1/audio/transcriptions", http.StripPrefix("/openai", modelProxy))
	handle("/openai/v1/models", http.HandlerFunc(h.getModels))

	// Add HTTP instrumentation for the whole server.
	h.Handler = otelhttp.NewHandler(mux, "/")

	return h
}

func sendErrorResponse(w http.ResponseWriter, status int, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("sending error response: %v: %v", status, msg)

	w.WriteHeader(status)

	if status >= 500 {
		// Don't leak internal error messages to the client.
		msg = http.StatusText(status)
	}

	if err := json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{
		Error: msg,
	}); err != nil {
		log.Printf("error encoding error response: %v", err)
	}
}
