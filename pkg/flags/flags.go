package flags

import (
	"dagger.io/dagger"
	"github.com/urfave/cli/v2"
)

func StringAsSecret(c *cli.Context, dag *dagger.Client, key string) *dagger.Secret {
	value := c.String(key)

	return dag.SetSecret(key, value)
}

func StringWithDefault(c *cli.Context, key, def string) string {
	value := c.String(key)
	if value == "" {
		return def
	}
	return value
}
