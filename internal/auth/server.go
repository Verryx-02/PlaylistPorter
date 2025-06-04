package auth

import (
	"fmt"
	"net/http"
	"time"
)

// StartHTTPServer starts a local HTTP server for OAuth callback
func StartHTTPServer(port string, codeChan chan string, errChan chan error) {
	// Setup HTTP handler
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("Received callback request: %s\n", r.URL.String())

		code := r.URL.Query().Get("code")
		if code == "" {
			errorMsg := r.URL.Query().Get("error")
			if errorMsg != "" {
				http.Error(w, fmt.Sprintf("Authorization error: %s", errorMsg), http.StatusBadRequest)
				errChan <- fmt.Errorf("authorization error: %s", errorMsg)
				return
			}
			http.Error(w, "No authorization code received", http.StatusBadRequest)
			errChan <- fmt.Errorf("no authorization code received")
			return
		}

		// Send success response
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`
			<!DOCTYPE html>
			<html>
			<head>
				<title>PlaylistPorter - Authorization Successful</title>
				<style>
					body { 
						font-family: Arial, sans-serif; 
						text-align: center; 
						margin-top: 50px; 
						background-color: #f0f2f5;
					}
					.container {
						background: white;
						max-width: 500px;
						margin: 0 auto;
						padding: 40px;
						border-radius: 10px;
						box-shadow: 0 2px 10px rgba(0,0,0,0.1);
					}
					.success { 
						color: #28a745; 
						font-size: 32px; 
						margin-bottom: 20px; 
					}
					.message { 
						color: #333; 
						font-size: 18px; 
						line-height: 1.6;
					}
				</style>
			</head>
			<body>
				<div class="container">
					<div class="success">Authorization Successful!</div>
					<div class="message">
						Your playlist migration can now continue.<br><br>
						<strong>You can close this browser window</strong> and return to the terminal.
					</div>
				</div>
			</body>
			</html>
		`))

		// Send code to channel
		fmt.Printf("Sending authorization code to channel\n")
		codeChan <- code
	})

	// Add a simple root handler for debugging
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`
				<!DOCTYPE html>
				<html>
				<head><title>PlaylistPorter OAuth Server</title></head>
				<body>
					<h1>🎵 PlaylistPorter OAuth Server</h1>
					<p>This server is running and waiting for OAuth callbacks.</p>
					<p>Callback endpoint: <code>http://localhost:8080/callback</code></p>
					<p>Status: <span style="color: green;">Ready</span></p>
				</body>
				</html>
			`))
		} else {
			http.NotFound(w, r)
		}
	})

	// Setup HTTP server (NOT HTTPS!)
	server := &http.Server{
		Addr:         ":" + port,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	fmt.Printf("Starting HTTP server on http://localhost:%s\n", port)
	fmt.Printf("Callback URL: http://localhost:%s/callback\n", port)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		errChan <- fmt.Errorf("HTTP server error: %w", err)
	}
}
