package main

import (
	"context"
	"fmt"
	"os"

	"github.com/concolorcarne/tinyrpc/app"
)

type getDirContentsRequest struct {
	Token string `validate:"nonzero"`
	Path  string
}

type directoryListingItem struct {
	Name  string `validate:"nonzero"`
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

const secretTokenValue = "123456"

func GetTokenMiddleware(ctx context.Context, req any, method string, handler app.MiddlewareHandler) (any, error) {
	token := app.GetHeader(ctx, "token")
	if token != secretTokenValue {
		return nil, fmt.Errorf("invalid secret token")
	}
	return handler(ctx, req)
}
func main() {
	a := app.New("localhost:8000", "./output.ts")
	app.NewRoute(getDirContents).AttachWithMiddleware(a, GetTokenMiddleware)
	app.NewRoute(sayHelloHandler).Attach(a)

	a.Start()
}
