package commands

import (
	"context"
	"fmt"

	"dagger.io/dagger"
	"github.com/hashicorp/go-multierror"
	"github.com/lukeshay/deployer/env"
	"github.com/lukeshay/deployer/sysexit"
	"github.com/magefile/mage/mg"
)

type Docker mg.Namespace

func (Docker) Build(ctx context.Context, repo string, branch string, latest bool, dockerRepo string) {
	ctx, dag := setupTarget(ctx)

	ghcrPassword := env.CreateRequired(ctx, dag, "GHCR_PASSWORD")
	ghcrUserName := env.CreateRequired(ctx, dag, "GHCR_USERNAME")

	var project *dagger.Directory

	if dockerRepo == "" {
		dockerRepo = repo
	}

	if repo == "." {
		project = dag.Host().Directory(".")
	} else {
		project = dag.
			Git(fmt.Sprintf("https://github.com/%s", repo)).
			Branch(branch).
			Tree()
	}

	tags := []string{branch}
	if latest {
		tags = append(tags, "latest")
	}

	buildWithAuth := project.
		DockerBuild().
		WithRegistryAuth("ghcr.io", ghcrUserName.Value(), ghcrPassword.Secret())

	var result *multierror.Error

	for _, tag := range tags {
		addr, err := buildWithAuth.Publish(ctx, fmt.Sprintf(
			"ghcr.io/%s:%s",
			dockerRepo,
			tag,
		), dagger.ContainerPublishOpts{
			MediaTypes: dagger.Dockermediatypes,
		})
		if err != nil {
			result = multierror.Append(result, err)
		} else {
			fmt.Println("Published image: ", addr)
		}
	}

	if err := result.ErrorOrNil(); err != nil {
		fmt.Printf("There was an error publishing the image: %v", err)
		sysexit.Os(err)
	}
}
