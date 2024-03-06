package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/concolorcarne/crosser/app"
)

type getDirContentsRequest struct {
	Token string
	Path  string
}

type directoryListingItem struct {
	Name  string
	IsDir bool
}

type getDirContentsResponse struct {
	Items        []directoryListingItem
	SomeOtherVal string
}

func getDirContents(ctx context.Context, req getDirContentsRequest) (*getDirContentsResponse, error) {
	// Do something with the token...
	dirContents, err := os.ReadDir(req.Path)
	if err != nil {
		return nil, fmt.Errorf("error looking up directory: %v", err)
	}

	items := []directoryListingItem{}
	for _, item := range dirContents {
		items = append(items, directoryListingItem{
			Name:  item.Name(),
			IsDir: item.IsDir(),
		})
	}
	return &getDirContentsResponse{
		Items: items,
	}, nil
}

func GetTokenMiddleware(ctx context.Context, headers http.Header) error {
	token := headers.Get("token")
	if token != "secret-token" {
		return fmt.Errorf("invalid secret token")
	}
	return nil
}

func main() {
	a := app.New("localhost:8000", "./output.ts")
	app.NewRoute(getDirContents).Attach(a, []app.HeaderMiddlewareFn{GetTokenMiddleware})

	a.AddAdditionalHandlers("/fe-dist/", http.StripPrefix("/fe-dist/", http.FileServer(http.Dir("./fe-dist/"))))

	a.Start()
}
