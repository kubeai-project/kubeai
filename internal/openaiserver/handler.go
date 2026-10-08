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

	// NOTE: Proxying all paths to backend engines is a security risk.
	// Make sure to only proxy paths that are safe to expose to the public.
	// Example: vLLM supports loading arbitrary model adapaters via the API
	// at `/v1/load_lora_adapter`.

	mux.Handle("/openai/v1/chat/completions", http.StripPrefix("/openai", modelProxy))
	mux.Handle("/openai/v1/completions", http.StripPrefix("/openai", modelProxy))
	mux.Handle("/openai/v1/embeddings", http.StripPrefix("/openai", modelProxy))
	mux.Handle("/v1/rerank", modelProxy)
	mux.Handle("/v1/systemone", modelProxy)
	mux.Handle("/v1/messages", modelProxy)
	mux.Handle("/openai/v1/rerank", deprecatedRoute("/v1/rerank", http.StripPrefix("/openai", modelProxy)))
	mux.Handle("/openai/v1/systemone", deprecatedRoute("/v1/systemone", http.StripPrefix("/openai", modelProxy)))
	mux.Handle("/openai/v1/audio/transcriptions", http.StripPrefix("/openai", modelProxy))
	mux.Handle("/openai/v1/responses", http.StripPrefix("/openai", modelProxy))
	mux.Handle("/openai/v1/models", http.HandlerFunc(h.getModels))
	mux.Handle("/v1/models", http.HandlerFunc(h.getModels))

	// Add HTTP instrumentation for the whole server.
	h.Handler = otelhttp.NewHandler(mux, "/")

	return h
}

// Keep legacy clients working during the migration. Removal is planned for the
// next release; do not redirect POST requests or invent a calendar sunset date.
func deprecatedRoute(successor string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Deprecation", "@1791417600") // 2026-10-08T00:00:00Z (RFC 9745).
		w.Header().Add("Link", "<"+successor+">; rel=\"successor-version\"")
		next.ServeHTTP(w, r)
	})
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
