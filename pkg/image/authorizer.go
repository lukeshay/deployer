package image

import (
	"fmt"

	"github.com/docker/docker/api/types/registry"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
)

type Authorizer struct {
	name string
}

func NewAuthorizer(name string) Authorizer {
	return Authorizer{
		name: name,
	}
}

func (a Authorizer) Authorize() (registry.AuthConfig, error) {
	ref, err := name.ParseReference(a.name)
	if err != nil {
		return registry.AuthConfig{}, fmt.Errorf("error parsing name: %s", err.Error())
	}

	auther, err := authn.DefaultKeychain.Resolve(ref.Context())
	if err != nil {
		return registry.AuthConfig{}, fmt.Errorf("error resolving source image auth: %s", err.Error())
	}
	sourceAuthConfig, err := auther.Authorization()
	if err != nil {
		return registry.AuthConfig{}, fmt.Errorf("error getting source image auth: %s", err.Error())
	}

	return registry.AuthConfig{
		Password:      sourceAuthConfig.Password,
		ServerAddress: ref.Context().RegistryStr(),
		Username:      sourceAuthConfig.Username,
	}, nil
}

func (a Authorizer) AuthorizeEncoded() (string, error) {
	authConfig, err := a.Authorize()
	if err != nil {
		return "", err
	}
	encodedAuthConfig, err := registry.EncodeAuthConfig(authConfig)
	if err != nil {
		return "", fmt.Errorf("error encoding source image auth: %s", err.Error())
	}

	return encodedAuthConfig, nil
}
