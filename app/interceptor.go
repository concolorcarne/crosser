package app

import "context"

type Interceptor = func(ctx context.Context, req any, method string, handler CollapsedInterceptor) (any, error)
type CollapsedInterceptor = func(ctx context.Context, req any) (any, error)

func collapseInterceptors(functions []Interceptor, method string, finalFn CollapsedInterceptor) CollapsedInterceptor {
	if len(functions) == 0 {
		return finalFn
	} else {
		return chainFunctions(functions, method, finalFn, 0)
	}
}

// This basically maps the interceptor type -> finalFn type, with the handler
// populated with the next function
func chainFunctions(functions []Interceptor, method string, finalFn CollapsedInterceptor, current int) CollapsedInterceptor {
	if current == len(functions) {
		return finalFn
	}

	return func(ctx context.Context, req any) (any, error) {
		currentFunction := functions[current]
		return currentFunction(ctx, req, method, chainFunctions(functions, method, finalFn, current+1))
	}
}
