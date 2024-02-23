package commands

import (
	"context"
	"fmt"
	"os"

	"dagger.io/dagger"
	"github.com/lukeshay/deployer/sysexit"
)

func setupTarget(ctx context.Context) (context.Context, *dagger.Client) {
	defer sysexit.Handle()

	var dag *dagger.Client
	var err error
	dag, ok := ctx.Value("dag").(*dagger.Client)
	if !ok {
		dag, err = dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
		ctx = context.WithValue(ctx, "dag", dag)
		mustBeAvailable(err)
		defer dag.Close()
	}

	return ctx, dag
}

func mustBeAvailable(err error) {
	if err != nil {
		panic(sysexit.Unavailable(err))
	}
}

func requireEnv(env string) string {
	val := os.Getenv(env)
	if val == "" {
		panic(sysexit.Config(fmt.Errorf("% is not set", env)))
	}

	return val
}
