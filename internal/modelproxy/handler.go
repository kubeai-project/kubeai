package modelproxy

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"

	v1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/kubeai-project/kubeai/internal/apiutils"
	"github.com/kubeai-project/kubeai/internal/metrics"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type ModelClient interface {
	LookupModel(ctx context.Context, model, adapter string, selectors []string) (*v1.Model, error)
	ScaleAtLeastOneReplica(ctx context.Context, model string) error
}

type LoadBalancer interface {
	AwaitBestAddress(ctx context.Context, req *apiutils.Request) (string, func(), error)
}

// Handler serves http requests for end-clients.
// It is also responsible for triggering scale-from-zero.
type Handler struct {
	modelClient  ModelClient
	loadBalancer LoadBalancer
	maxRetries   int
	retryCodes   map[int]struct{}
}

func NewHandler(
	modelClient ModelClient,
	loadBalancer LoadBalancer,
	maxRetries int,
	retryCodes map[int]struct{},
) *Handler {
	return &Handler{
		modelClient:  modelClient,
		loadBalancer: loadBalancer,
		maxRetries:   maxRetries,
		retryCodes:   retryCodes,
	}
}

var defaultRetryCodes = map[int]struct{}{
	http.StatusInternalServerError: {},
	http.StatusBadGateway:          {},
	http.StatusServiceUnavailable:  {},
	http.StatusGatewayTimeout:      {},
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == apiutils.MessagesPath && r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		pr := &proxyRequest{http: r}
		pr.sendErrorResponse(w, http.StatusMethodNotAllowed, "messages requires POST")
		return
	}
	if r.URL.Path == apiutils.SystemOnePath && r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "systemone requires POST", http.StatusMethodNotAllowed)
		return
	}
	log.Printf("url: %v", r.URL)

	w.Header().Set("X-Proxy", "lingo")

	pr, err := h.parseProxyRequest(r)
	if err != nil {
		if errors.Is(err, apiutils.ErrRequestTooLarge) {
			pr.sendErrorResponse(w, http.StatusRequestEntityTooLarge, "%v", err)
		} else if errors.Is(err, apiutils.ErrBadRequest) {
			pr.sendErrorResponse(w, http.StatusBadRequest, "%v", err)
		} else if errors.Is(err, apiutils.ErrModelNotFound) {
			pr.sendErrorResponse(w, http.StatusNotFound, "%v", err)
		} else {
			pr.sendErrorResponse(w, http.StatusInternalServerError, "parsing request: %v", err)
		}
		return
	}

	log.Println("model:", pr.Model, "adapter:", pr.Adapter)

	metricAttrs := metric.WithAttributeSet(attribute.NewSet(
		metrics.AttrRequestModel.String(pr.RequestedModel),
		metrics.AttrRequestType.String(metrics.AttrRequestTypeHTTP),
	))
	metrics.InferenceRequestsActive.Add(pr.http.Context(), 1, metricAttrs)
	defer metrics.InferenceRequestsActive.Add(pr.http.Context(), -1, metricAttrs)

	// Ensure the backend is scaled to at least one Pod.
	if err := h.modelClient.ScaleAtLeastOneReplica(r.Context(), pr.Model); err != nil {
		pr.sendErrorResponse(w, http.StatusInternalServerError, "unable to scale model: %v", err)
		return
	}

	h.proxyHTTP(w, pr)
}

// AdditionalProxyRewrite is an injection point for modifying proxy requests.
// Used in tests.
var AdditionalProxyRewrite = func(*httputil.ProxyRequest) {}

func (h *Handler) proxyHTTP(w http.ResponseWriter, pr *proxyRequest) {
	log.Printf("Waiting for host: %v", pr.ID)

	addr, decrementInflight, err := h.loadBalancer.AwaitBestAddress(pr.http.Context(), pr.Request)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			pr.sendErrorResponse(w, http.StatusInternalServerError, "request cancelled while finding host: %v", err)
			return
		case errors.Is(err, context.DeadlineExceeded):
			pr.sendErrorResponse(w, http.StatusGatewayTimeout, "request timeout while finding host: %v", err)
			return
		default:
			pr.sendErrorResponse(w, http.StatusGatewayTimeout, "unable to find host: %v", err)
			return
		}
	}
	// NOTE: decrementInflight will be called after the request succeeds or fails after all retries.
	defer decrementInflight()

	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(&url.URL{
				Scheme: "http",
				Host:   addr,
			})
			r.Out.Host = r.In.Host
			AdditionalProxyRewrite(r)
		},
	}

	proxy.ModifyResponse = func(r *http.Response) error {
		// Record the response for metrics.
		pr.status = r.StatusCode

		// This point is reached if a response code is received.
		if h.isRetryCode(r.StatusCode) && pr.attempt < h.maxRetries {
			// Returning an error will trigger the ErrorHandler.
			return ErrRetry
		}

		return nil
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		// This point could be reached if a bad response code was sent by the backend
		// or
		// if there was an issue with the connection and no response was ever received.
		if err != nil && r.Context().Err() == nil && pr.attempt < h.maxRetries {
			pr.attempt++

			log.Printf("Retrying request (%v/%v): %v: %v", pr.attempt, h.maxRetries, pr.ID, err)
			h.proxyHTTP(w, pr)
			return
		}

		if !errors.Is(err, ErrRetry) {
			pr.sendErrorResponse(w, http.StatusBadGateway, "proxy: exceeded retries: %v/%v", pr.attempt, h.maxRetries)
		}
	}

	log.Printf("Proxying request to ip %v: %v\n", addr, pr.ID)
	proxy.ServeHTTP(w, pr.httpRequest())
}

var ErrRetry = errors.New("retry")

func (h *Handler) isRetryCode(status int) bool {
	var retry bool
	// TODO: avoid the nil check here and set a default map in the constructor.
	if h.retryCodes != nil {
		_, retry = h.retryCodes[status]
	} else {
		_, retry = defaultRetryCodes[status]
	}
	return retry
}
