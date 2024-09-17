package webserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/tcuthbert/apiserver/apiresponse"
)

var (
	MaxActiveAPIRequests = 3

	MaxAPIResponseTimeout = 60 * time.Second
	MaxReadTimeout        = 15 * time.Second
	MaxWriteTimeout       = 30 * time.Second
	MaxIdleTimeout        = 120 * time.Second
)

func Start(listenAddr *string, apiURL string) error {
	logger := log.New(os.Stdout, "webserver: ", log.LstdFlags)

	done := make(chan bool, 1)
	quit := make(chan os.Signal, 1)

	signal.Notify(quit, os.Interrupt)

	server := newWebserver(listenAddr, apiURL, logger)
	go gracefullShutdown(server, logger, quit, done)

	logger.Printf("Server is ready to handle requests at: %s", *listenAddr)

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("could not listen on %s: %w", *listenAddr, err)
	}

	<-done
	logger.Println("Server stopped")

	return nil
}

func gracefullShutdown(
	server *http.Server,
	logger *log.Logger,
	quit <-chan os.Signal,
	done chan<- bool,
) {
	<-quit
	logger.Println("Server is shutting down...")

	shutDownTime := 30 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), shutDownTime)
	defer cancel()

	server.SetKeepAlivesEnabled(false)

	if err := server.Shutdown(ctx); err != nil {
		logger.Fatalf("Failed to gracefully shutdown the server: %v\n", err)
	}

	close(done)
}

type RateLimiter struct {
	handler http.Handler
	logger  *log.Logger
	sem     chan (struct{})
}

func NewRateLimitHandler(handler http.Handler, logger *log.Logger, size int) *RateLimiter {
	return &RateLimiter{logger: logger, handler: handler, sem: make(chan struct{}, size)}
}

func (rl *RateLimiter) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	if !rl.acquire() { // too many in-flight requests detected.
		delay := max(1, rand.IntN(5)) // minimum 1s back-off delay.
		rl.logger.Printf(
			"WARNING: %ds back-off delay triggered: active-requests=%d max-request=%d",
			delay,
			rl.total(),
			rl.size(),
		)
		time.Sleep(time.Duration(delay) * time.Second)
	}
	defer rl.release()

	rl.handler.ServeHTTP(rw, r)
}

func (rl *RateLimiter) acquire() bool {
	rl.sem <- struct{}{}
	return rl.total() < rl.size()
}

func (rl *RateLimiter) release() {
	<-rl.sem
}

func (rl *RateLimiter) size() int {
	return cap(rl.sem)
}

func (rl *RateLimiter) total() int {
	return len(rl.sem)
}

type ApiRequestHandler struct {
	logger *log.Logger
	apiURL string
}

func (ah *ApiRequestHandler) handleRequest(
	resultCh chan error,
	rw http.ResponseWriter,
	r *http.Request,
) {
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		resultCh <- fmt.Errorf("api client error: %w", err)
		return
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		resultCh <- fmt.Errorf("failed to read upstream response body: %v", err)
		return
	}

	var repos apiresponse.Repos
	if err := json.Unmarshal(b, &repos); err != nil {
		resultCh <- fmt.Errorf("failed to unmarshal upstream response: %v: %q", err, b)
		return
	}

	enc := json.NewEncoder(rw)
	if err := enc.Encode(repos); err != nil {
		resultCh <- fmt.Errorf("failed to encode response: %v", err)
		return
	}

	close(resultCh)
}

func (ah *ApiRequestHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	start := time.Now()

	ctx, cancel := context.WithTimeout(r.Context(), MaxAPIResponseTimeout) // TODO: mdn timeouts
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ah.apiURL, nil)
	if err != nil {
		ah.logger.Printf("ERROR: api request error: %v", err)
		http.Error(
			rw,
			http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError,
		)
		return
	}

	resultCh := make(chan error, 1)
	go ah.handleRequest(resultCh, rw, req)

	// TODO: structured logging with slog
	select {
	case <-ctx.Done():
		ah.logger.Printf(
			"ERROR: request=%s response-time=%s: %v",
			req.URL,
			time.Since(start),
			ctx.Err(),
		)
		http.Error(rw, http.StatusText(http.StatusGatewayTimeout), http.StatusGatewayTimeout)
	case err := <-resultCh:
		if err != nil {
			ah.logger.Printf(
				"ERROR: response-time=%s: %v",
				time.Since(start),
				err,
			)
			http.Error(rw, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
		} else {
			ah.logger.Printf("INFO: response-time=%s", time.Since(start))
		}
	}
}

func newWebserver(listenAddr *string, apiURL string, logger *log.Logger) *http.Server {
	apiHandler := NewRateLimitHandler(
		&ApiRequestHandler{
			logger: logger,
			apiURL: apiURL,
		},
		logger,
		MaxActiveAPIRequests,
	)

	router := http.NewServeMux()
	router.Handle("/",
		http.TimeoutHandler(
			apiHandler,
			MaxAPIResponseTimeout,
			http.StatusText(http.StatusRequestTimeout),
		))

	router.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// TODO: use mdn recommended timeout values
	return &http.Server{
		Addr:         *listenAddr,
		Handler:      router,
		ErrorLog:     logger,
		ReadTimeout:  MaxReadTimeout,
		WriteTimeout: MaxWriteTimeout,
		IdleTimeout:  MaxIdleTimeout,
	}
}
