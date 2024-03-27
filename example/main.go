package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/concolorcarne/tinyrpc/app"
)

type getDirContentsRequest struct {
	Token string `validate:"required"`
	Path  string
}

type directoryListingItem struct {
	Name  string `validate:"required"`
	IsDir bool
}

type getDirContentsResponse struct {
	Items        []directoryListingItem
	SomeOtherVal string
}

func getDirContents(ctx context.Context, req getDirContentsRequest) (*getDirContentsResponse, error) {
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

type sayHelloRequest struct {
	Name string `validate:"required" json:"input_name"`
}

type sayHelloResponse struct {
	Message string `validate:"required"`
}

func sayHelloHandler(_ context.Context, req sayHelloRequest) (*sayHelloResponse, error) {
	return &sayHelloResponse{
		Message: fmt.Sprintf("Hello, %s!", req.Name),
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
	app.NewRoute(getDirContents).AttachWithMiddleware(a)
	app.NewRoute(sayHelloHandler).Attach(a)

	a.Start()
}
