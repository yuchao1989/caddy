package admin

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/mholt/caddy/app"
	"github.com/mholt/caddy/config"
	"github.com/mholt/caddy/server"
)

func TestServerList(t *testing.T) {
	caddyfile := testAddr
	w, r, p := setUp(t, caddyfile, "GET", "/", nil)

	serverList(w, r, p)

	if expected, actual := http.StatusOK, w.Code; expected != actual {
		t.Errorf("Expected status %d, got %d", expected, actual)
	}
	if expected, actual := "application/json", w.Header().Get("Content-Type"); expected != actual {
		t.Errorf("Expected Content-Type: %s, got %s", expected, actual)
	}
	if w.Body.Len() == 0 {
		t.Errorf("Expected response body to be non-empty")
	}
}

func TestServersCreate(t *testing.T) {
	caddyfile := testAddr
	newServerAddr := "127.0.0.1:3932"
	reqCaddyfile := newServerAddr + `
		gzip`
	w, r, p := setUp(t, caddyfile, "POST", "/", strings.NewReader(reqCaddyfile))

	serversCreate(w, r, p)

	if expected, actual := http.StatusAccepted, w.Code; expected != actual {
		t.Errorf("Expected status %d, got %d", expected, actual)
	}
	if expected, actual := 2, len(app.Servers); expected != actual {
		t.Fatalf("Expected %d servers, got %d", expected, actual)
	}
	if expected, actual := 1, len(app.Servers[0].Vhosts); expected != actual {
		t.Fatalf("Expected %d virtualhost on %s, got %d", expected, app.Servers[0].Address, actual)
	}
	if expected, actual := 1, len(app.Servers[1].Vhosts); expected != actual {
		t.Fatalf("Expected %d virtualhost on %s, got %d", expected, app.Servers[1].Address, actual)
	}
	if app.Servers[1].Vhosts["127.0.0.1"].Config.HandlerMap["gzip"] == nil {
		t.Error("Expected the servers be properly configured, but they weren't")
	}

	// Try a real request to the new server
	_, err := http.Get("http://" + newServerAddr)
	if err != nil {
		t.Errorf("Expected GET request to %s to succeed, but error was: %v", newServerAddr, err)
	}

	killServers()
}

func TestServersReplace(t *testing.T) {
	caddyfile := testAddr
	newServerAddr := "127.0.0.1:3932"
	reqCaddyfile := newServerAddr + `
	    gzip`
	w, r, p := setUp(t, caddyfile, "PUT", "/", strings.NewReader(reqCaddyfile))

	serversReplace(w, r, p)

	if expected, actual := http.StatusAccepted, w.Code; expected != actual {
		t.Errorf("Expected status %d, got %d", expected, actual)
	}
	if expected, actual := 1, len(app.Servers); expected != actual {
		t.Fatalf("Expected %d server, got %d", expected, actual)
	}
	if expected, actual := 1, len(app.Servers[0].Vhosts); expected != actual {
		t.Fatalf("Expected %d virtualhost on %s, got %d", expected, app.Servers[0].Address, actual)
	}
	if app.Servers[0].Vhosts["127.0.0.1"].Config.HandlerMap["gzip"] == nil {
		t.Error("Expected the servers be properly configured, but they weren't")
	}

	// Try a real request to the replacement server
	_, err := http.Get("http://" + newServerAddr)
	if err != nil {
		t.Errorf("Expected GET request to %s to succeed, but error was: %v", newServerAddr, err)
	}

	killServers()
}

func TestServerInfo(t *testing.T) {
	caddyfile := testAddr
	w, r, p := setUp(t, caddyfile, "GET", "/"+testAddr, nil)

	serverInfo(w, r, p)

	if expected, actual := http.StatusOK, w.Code; expected != actual {
		t.Errorf("Expected status %d, got %d", expected, actual)
	}
	if expected, actual := "application/json", w.Header().Get("Content-Type"); expected != actual {
		t.Errorf("Expected Content-Type: %s, got %s", expected, actual)
	}
	if w.Body.Len() == 0 {
		t.Errorf("Expected response body to be non-empty")
	}
}

func TestServerStop(t *testing.T) {
	testServerAddr := "localhost:6099"
	caddyfile := testServerAddr
	w, r, p := setUp(t, caddyfile, "DELETE", "/"+testServerAddr, nil)

	properlyStopped := make(chan bool, 1)
	app.Servers[0].Vhosts["localhost"].Config.Shutdown = append(app.Servers[0].Vhosts["localhost"].Config.Shutdown, func() error {
		properlyStopped <- true
		return nil
	})

	go app.Servers[0].Start()
	serverStop(w, r, p)

	if expected, actual := http.StatusAccepted, w.Code; expected != actual {
		t.Errorf("Expected status %d, got %d", expected, actual)
	}

	resp, err := http.Get("http://" + testServerAddr)
	if err == nil {
		t.Errorf("Expected server to be shut down, but GET request succeeded: %s", resp.Status)
	}

	select {
	case <-properlyStopped:
	case <-time.After(app.ShutdownCutoff):
		t.Errorf("Shutdown callback was not executed")
	}

	killServers()
}

//
// HELPFUL TEST UTILITY FUNCTIONS BELOW
//

// setUp sets up a test by creating the test server(s) according to
// the contents of caddyfile, then prepares a request according to
// the method, path, and body that is passed in. It also returns a
// ResponseRecorder for use in checking the response. It does NOT
// start any listeners.
func setUp(t *testing.T, caddyfile, method, path string, body io.Reader) (*httptest.ResponseRecorder, *http.Request, httprouter.Params) {
	makeTestServer(t, caddyfile)
	req, err := http.NewRequest(method, path, body)
	if err != nil {
		t.Fatalf("Error creating request: %v", err)
	}
	w := httptest.NewRecorder()
	_, param, _ := router.Lookup(method, path)
	return w, req, param
}

// makeTestServer clears app.Servers and then populates it
// according to the contents of the caddyfile. It does NOT
// start the listeners.
func makeTestServer(t *testing.T, caddyfile string) {
	app.Servers = []*server.Server{} // start empty each time

	configs, err := config.Load("Testfile", strings.NewReader(caddyfile))
	if err != nil {
		t.Fatalf("Could not create server configs: %v", err)
	}

	// Arrange it by bind address (resolve hostname)
	bindings, err := config.ArrangeBindings(configs)
	if err != nil {
		t.Fatalf("Could not arrange test server bindings: %v", err)
	}

	for address, cfgs := range bindings {
		// Create a server that will build the virtual host
		s, err := server.New(address.String(), cfgs, cfgs[0].TLS.Enabled)
		if err != nil {
			t.Fatalf("Could not create test server %s: %v", address, err)
		}

		// See if there's a server that is already listening at the address
		var hasListener bool
		for _, existingServer := range app.Servers {
			if address.String() == existingServer.Address {
				hasListener = true

				// The virtual host address must not exist already
				if _, vhostExists := existingServer.Vhosts[cfgs[0].Host]; vhostExists {
					t.Fatalf("Virtualhost already exists: %s", cfgs[0].Address())
				}

				vh := s.Vhosts[cfgs[0].Host]
				existingServer.Vhosts[cfgs[0].Host] = vh
				break
			}
		}

		if !hasListener {
			// Initiate the new server that will operate the listener for this virtualhost
			app.Servers = append(app.Servers, s)
		}
	}
}

// killServers immediately and forcefully stops all
// servers but does not delete them. Call this function
// after tests that start listeners.
func killServers() {
	for _, serv := range app.Servers {
		serv.Stop(0)
	}
}

const testAddr = "localhost:2015"
