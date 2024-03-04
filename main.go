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

func main() {
	a := app.New("localhost:8000", "./output.ts")
	app.NewQuery(getDirContents).Attach(a)

	a.AddAdditionalHandlers("/fe-dist/", http.StripPrefix("/fe-dist/", http.FileServer(http.Dir("./fe-dist/"))))

	a.Start()
}
