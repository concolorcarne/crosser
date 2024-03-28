package app

import "context"

type MiddlewareFn = func(ctx context.Context, req any, method string, handler MiddlewareHandler) (any, error)
type MiddlewareHandler = func(ctx context.Context, req any) (any, error)

func collapseMiddleware(functions []MiddlewareFn, method string, finalFn MiddlewareHandler) MiddlewareHandler {
	if len(functions) == 0 {
		return finalFn
	} else {
		return chainFunctions(functions, method, finalFn, 0)
	}
}

// This basically maps the interceptor type -> finalFn type, with the handler
// populated with the next function
func chainFunctions(functions []MiddlewareFn, method string, finalFn MiddlewareHandler, current int) MiddlewareHandler {
	if current == len(functions) {
		return finalFn
	}

	return func(ctx context.Context, req any) (any, error) {
		currentFunction := functions[current]
		return currentFunction(ctx, req, method, chainFunctions(functions, method, finalFn, current+1))
	}
}
