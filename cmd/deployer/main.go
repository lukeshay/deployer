package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"dagger.io/dagger"
	"github.com/hashicorp/go-multierror"
	"github.com/lukeshay/deployer/pkg/flags"
	"github.com/lukeshay/deployer/pkg/sysexit"
	"github.com/urfave/cli/v2"
)

var (
	version         = "dev"
	deployerLogFile = createLogFile("deployer")
	daggerLogFile   = createLogFile("dagger")
)

func configureLog(cli *cli.Context) {
	writers := []io.Writer{deployerLogFile}

	if cli.Bool("verbose") {
		writers = append(writers, os.Stderr)
	}
	writer := io.MultiWriter(writers...)
	slog.SetDefault(
		slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})),
	)
}

func main() {
	defer sysexit.Handle()
	interruptChannel := make(chan os.Signal, 1)
	signal.Notify(interruptChannel, os.Interrupt)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-interruptChannel
		cancel()
	}()

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not get current directory: %v\n", err)

		panic(sysexit.CreateNew(err, 1))
	}

	app := &cli.App{
		Name:    "deployer",
		Usage:   "Deploy your code using dagger",
		Version: version,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name: "verbose",
			},
			&cli.StringFlag{
				Name:  "cwd",
				Value: cwd,
			},
		},
		Before: func(c *cli.Context) error {
			configureLog(c)

			return nil
		},
		Commands: []*cli.Command{
			{
				Name:  "docker",
				Usage: "run docker commands in Dagger",
				Subcommands: []*cli.Command{
					{
						Name:  "build",
						Usage: "build a Docker image",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:     "repository",
								Aliases:  []string{"r", "repo"},
								Usage:    "the github repository to clone: <owner>/<repo>",
								Required: true,
							},
							&cli.StringFlag{
								Name:     "ref",
								Usage:    "the git ref to checkout",
								Required: true,
							},
							&cli.StringSliceFlag{
								Aliases: []string{"t"},
								Name:    "tags",
								Usage:   "the tags to apply to the image",
								Value:   &cli.StringSlice{},
							},
							&cli.BoolFlag{
								Aliases: []string{"p"},
								Name:    "publish",
								Usage:   "whether to publish the image",
								Value:   false,
							},
							&cli.BoolFlag{
								Name:    "is-local",
								Aliases: []string{"l", "local"},
								Usage:   "whether to use the local directory instead of cloning the repository",
								Value:   false,
							},
							&cli.StringFlag{
								Aliases: []string{"dr", "docker-repo"},
								Name:    "docker-repository",
								Usage:   "the repository to push the image to",
							},
							&cli.StringFlag{
								Aliases:  []string{"du"},
								Name:     "docker-username",
								Usage:    "the username to authenticate with GHCR",
								Required: true,
								EnvVars:  []string{"GHCR_USERNAME", "DOCKER_USERNAME"},
							},
							&cli.StringFlag{
								Aliases:  []string{"dp"},
								Name:     "docker-password",
								Usage:    "the password to authenticate with GHCR",
								Required: true,
								EnvVars:  []string{"GHCR_PASSWORD", "DOCKER_PASSWORD"},
							},
						},
						Action: func(c *cli.Context) error {
							ctx, dag, err := initializeDagger(c.Context)
							if err != nil {
								return cli.Exit(err, 1)
							}

							c.Context = ctx

							repository := c.String("repository")
							ref := c.String("ref")
							tags := append(c.StringSlice("tags"), ref)
							dockerRepository := flags.StringWithDefault(c, "docker-repository", fmt.Sprintf("ghcr.io/%s", repository))
							dockerPassword := flags.StringAsSecret(c, dag, "docker-password")
							dockerUserName := c.String("docker-username")
							local := c.Bool("local")
							publish := c.Bool("publish")

							dockerRepositoryUrl, err := url.Parse(dockerRepository)
							if err != nil {
								slog.Error("Could not parse docker repository", "dockerRepository", dockerRepository, "error", err)
								return cli.Exit(err, 1)
							}

							outFile := fmt.Sprintf(".deployer/artifacts/images/%s.tar", dockerRepository)

							if err := os.MkdirAll(filepath.Dir(outFile), 0755); err != nil {
								slog.Error("Could not create directory", "outFile", outFile, "error", err)
								return cli.Exit(err, 1)
							}

							var project *dagger.Directory

							if local {
								project = dag.Host().Directory(".")
							} else {
								project = dag.
									Git(fmt.Sprintf("https://github.com/%s", repository)).
									Branch(ref).
									Tree()
							}

							image := project.
								DockerBuild().
								WithRegistryAuth(dockerRepositoryUrl.Host, dockerUserName, dockerPassword).
								WithLabel("org.opencontainers.image.source", fmt.Sprintf("https://github.com/%s", repository)).
								WithLabel("org.opencontainers.image.created", time.Now().Format(time.RFC3339)).
								WithLabel("org.opencontainers.image.revision", ref)

							_, err = image.Export(c.Context, outFile)
							if err != nil {
								return cli.Exit(err, 1)
							}

							fmt.Printf("Built image: %s\n", outFile)

							if publish {
								var result *multierror.Error

								imageWithAuth := image.WithRegistryAuth(dockerRepositoryUrl.Host, dockerUserName, dockerPassword)

								for _, tag := range tags {
									addr, err := imageWithAuth.Publish(ctx, fmt.Sprintf(
										"%s:%s",
										dockerRepository,
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
									return cli.Exit(err, 1)
								}
							}

							return nil
						},
					},
				},
			},
		},
	}

	if err := app.RunContext(ctx, os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error running command: %v\n", err)

		panic(sysexit.CreateNew(err, 1))
	}
}

var dagCtxKey = "dag"

func initializeDagger(ctx context.Context) (context.Context, *dagger.Client, error) {
	var dag *dagger.Client
	dag, ok := ctx.Value(dagCtxKey).(*dagger.Client)
	if !ok {
		var err error
		dag, err = dagger.Connect(ctx, dagger.WithLogOutput(daggerLogFile))
		if err != nil {
			return ctx, nil, err
		}

		ctx = context.WithValue(ctx, dagCtxKey, dag)
		defer dag.Close()
	}

	return ctx, dag, nil
}

func createLogFile(name string) *os.File {
	defer sysexit.Handle()

	err := os.MkdirAll(".deployer/logs", 0755)
	if err != nil {
		panic(sysexit.Create(err))
	}

	logFile, err := os.CreateTemp(".deployer/logs", fmt.Sprintf("%s-*.log", name))
	if err != nil {
		panic(sysexit.Create(err))
	}

	return logFile
}
