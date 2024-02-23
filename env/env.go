package env

import (
	"context"
	"fmt"
	"os"

	"dagger.io/dagger"
	"github.com/lukeshay/deployer/sysexit"
)

type Env struct {
	dag    *dagger.Client
	secret *dagger.Secret
	ctx    context.Context
	key    string
	value  string
}

func CreateRequired(ctx context.Context, dag *dagger.Client, key string) *Env {
	val := os.Getenv(key)
	if val == "" {
		panic(sysexit.Config(fmt.Errorf("% is not set", key)))
	}

	secret := dag.SetSecret(key, val)

	return &Env{
		ctx:    ctx,
		key:    key,
		dag:    dag,
		secret: secret,
		value:  val,
	}
}

func (e *Env) Value() string {
	return e.value
}

func (e *Env) Secret() *dagger.Secret {
	return e.secret
}
