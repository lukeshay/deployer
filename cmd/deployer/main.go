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
	"strings"
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
				Name:  "dagger",
				Usage: "run commands in dagger",
				Subcommands: []*cli.Command{
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
										Usage:    "the repository to push the image to",
										Required: true,
									},
									&cli.StringSliceFlag{
										Name:  "tags",
										Usage: "the tags to apply to the image",
										Value: &cli.StringSlice{},
									},
									&cli.BoolFlag{
										Name:  "publish",
										Usage: "whether to publish the image",
										Value: false,
									},
									&cli.StringFlag{
										Name:    "username",
										Usage:   "the username to authenticate with Docker",
										EnvVars: []string{"DEPLOYER_DOCKER_USERNAME"},
									},
									&cli.StringFlag{
										Name:    "password",
										Usage:   "the password to authenticate with Docker",
										EnvVars: []string{"DEPLOYER_DOCKER_PASSWORD"},
									},
									&cli.StringSliceFlag{
										Name:  "labels",
										Usage: "labels to apply to the image in the form of key=value",
										Value: &cli.StringSlice{},
									},
									&cli.StringFlag{
										Name:  "pull",
										Usage: "pull an image to build from",
									},
								},
								Action: func(c *cli.Context) error {
									ctx, dag, err := initializeDagger(c.Context)
									if err != nil {
										return cli.Exit(err, 1)
									}

									c.Context = ctx

									repository := c.String("repository")
									tags := c.StringSlice("tags")
									password := flags.StringAsSecret(c, dag, "password")
									username := c.String("username")
									publish := c.Bool("publish")
									labels := c.StringSlice("labels")
									pull := c.String("pull")

									repositoryUrl, err := url.Parse(repository)
									if err != nil {
										slog.Error("Could not parse docker repository", "dockerRepository", repository, "error", err)
										return cli.Exit(err, 1)
									}

									outFile := fmt.Sprintf(".deployer/artifacts/images/%s.tar", repository)

									if err := os.MkdirAll(filepath.Dir(outFile), 0755); err != nil {
										slog.Error("Could not create directory", "outFile", outFile, "error", err)
										return cli.Exit(err, 1)
									}

									if pull != "" {
										dag.
											Container().
											WithRegistryAuth(repositoryUrl.Host, username, password).
											From(pull)
									}

									project := dag.Host().Directory(".")

									image := project.
										DockerBuild().
										WithRegistryAuth(repositoryUrl.Host, username, password).
										WithLabel("org.opencontainers.image.created", time.Now().Format(time.RFC3339))

									for _, label := range labels {
										parts := strings.SplitN(label, "=", 2)

										if len(parts) != 2 {
											slog.Error("Invalid label", "label", label)
										} else {
											image = image.WithLabel(parts[0], parts[1])
										}
									}

									_, err = image.Export(c.Context, outFile)
									if err != nil {
										return cli.Exit(err, 1)
									}

									fmt.Printf("Built image: %s\n", outFile)

									if publish {
										var result *multierror.Error

										imageWithAuth := image.WithRegistryAuth(repositoryUrl.Host, username, password)

										for _, tag := range tags {
											addr, err := imageWithAuth.Publish(ctx, fmt.Sprintf(
												"%s:%s",
												repository,
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
