package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"golang.org/x/sync/errgroup"
)

type Manager struct {
	sync.Mutex
	projectID     string
	state         string
	config        *oauth2.Config
	cancel        context.CancelFunc
	token         *oauth2.Token
	c             chan *http.Client
	endpoint      string
	tokenLocation string
}

func NewManager(projectID string, credentials, tokenLocation, endpoint string) (*Manager, error) {
	b, err := os.ReadFile(credentials)
	if err != nil {
		return nil, fmt.Errorf("failed to read credentials file: %w", err)
	}

	config, err := google.ConfigFromJSON(b, "https://www.googleapis.com/auth/sdm.service")
	if err != nil {
		return nil, fmt.Errorf("failed to create auth config with SDM scope: %w", err)
	}

	config.Endpoint.AuthURL = fmt.Sprintf("https://nestservices.google.com/partnerconnections/%s/auth", projectID)
	config.RedirectURL = endpoint + "/oauth2/redirect"

	return &Manager{
		Mutex:         sync.Mutex{},
		config:        config,
		c:             make(chan *http.Client),
		tokenLocation: tokenLocation,
		endpoint:      endpoint,
	}, nil
}

func (a *Manager) GetClient() <-chan *http.Client {
	return a.c
}

func (a *Manager) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	group, ctx := errgroup.WithContext(ctx)

	group.Go(func() error {
		return a.runWeb(ctx)
	})
	group.Go(func() error {
		return a.checkForStoredTokenClient(ctx, a.c)
	})

	return group.Wait()
}

func (a *Manager) runWeb(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/start", func(writer http.ResponseWriter, request *http.Request) {
		a.Lock()
		defer a.Unlock()
		a.state = uuid.New().String()
		http.Redirect(writer, request, a.config.AuthCodeURL(a.state, oauth2.AccessTypeOffline), http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/oauth2/redirect", func(writer http.ResponseWriter, request *http.Request) {
		code := request.URL.Query().Get("code")
		f, err := os.OpenFile("token", os.O_TRUNC|os.O_CREATE|os.O_RDWR, 0755)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}

		token, err := a.config.Exchange(ctx, code)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		err = json.NewEncoder(f).Encode(token)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		a.token = token

		client := a.config.Client(ctx, token)
		select {
		case <-ctx.Done():
			writer.WriteHeader(http.StatusGone)
		case <-request.Context().Done():
			writer.WriteHeader(http.StatusGone)
		case a.c <- client:
			writer.WriteHeader(http.StatusOK)
			writer.Write([]byte("done"))
		}
	})
	group, ctx := errgroup.WithContext(ctx)
	server := http.Server{Addr: ":8080", Handler: mux}
	group.Go(func() error {
		return server.ListenAndServe()
	})
	group.Go(func() error {
		<-ctx.Done()
		return server.Shutdown(context.Background())
	})
	return group.Wait()
}

func (a *Manager) checkForStoredTokenClient(ctx context.Context, c chan<- *http.Client) error {
	tok, err := getTokenFromFS(a.tokenLocation)
	if err != nil {
		fmt.Println("please go to " + a.endpoint + "/oauth2/start")
		return nil
	}
	a.token = tok

	client := a.config.Client(ctx, tok)
	select {
	case <-ctx.Done():
		return nil
	case c <- client:
		return nil
	}

}

func (a *Manager) Close() error {
	if a.cancel != nil {
		a.cancel()
	}
	return nil
}

func getTokenFromFS(location string) (*oauth2.Token, error) {
	f, err := os.Open(location)
	if err != nil {
		return nil, fmt.Errorf("failed to open token file: %w", err)
	}
	defer f.Close()
	var tok oauth2.Token
	err = json.NewDecoder(f).Decode(&tok)
	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}
	return &tok, nil
}
