package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"dagger.io/dagger"
	"github.com/hashicorp/go-multierror"
	"github.com/lukeshay/deployer/pkg/image"
	"github.com/lukeshay/deployer/pkg/sysexit"
	"github.com/sean-/sysexits"
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
				Name:        "version",
				Description: "Prints the version",
				Action: func(cli *cli.Context) error {
					fmt.Printf("deployer.%s\n", version)
					return nil
				},
			},
			{
				Name:  "dagger",
				Usage: "run commands in dagger",
				Before: func(c *cli.Context) error {
					ctx, _, err := initializeDagger(c.Context)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Could not initialize dagger: %v\n", err)
						return err
					}

					c.Context = ctx

					return nil
				},
				After: func(c *cli.Context) error {
					dag, err := getDaggerFromContext(c.Context)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Could not get dagger from context: %v\n", err)
						return err
					}

					if err := dag.Close(); err != nil {
						fmt.Fprintf(os.Stderr, "Could not close dagger: %v\n", err)
						return err
					}

					return nil
				},
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
										Name:     "name",
										Usage:    "the name of the image including the registry",
										Required: true,
									},
									&cli.StringFlag{
										Name:     "identifier",
										Usage:    "a unique identifier for the image",
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
									dag, err := getDaggerFromContext(c.Context)
									if err != nil {
										slog.Error("Dagger engine not in context")
										return cli.Exit(errorMessage(err, "Could not get Dagger engine"), sysexits.Software)
									}

									c.Context = ctx

									name := c.String("name")
									tags := c.StringSlice("tags")
									publish := c.Bool("publish")
									labels := c.StringSlice("labels")
									pull := c.String("pull")
									identifier := c.String("identifier")

									tags = append(tags, identifier)

									img := image.NewImage(image.ImageOptions{
										Name:      name,
										UniqueTag: identifier,
										Tags:      tags,
									})
									authorizer := image.NewAuthorizer(name)
									outFile := fmt.Sprintf(".deployer/artifacts/images/%s.tar", img.Name())

									if err := os.MkdirAll(filepath.Dir(outFile), 0755); err != nil {
										slog.Error("Could not create directory", "outFile", outFile, "error", err)
										return cli.Exit(errorMessage(err, "Could not create directory"), sysexits.NoPerm)
									}

									if pull != "" {
										pullAuthorizer := image.NewAuthorizer(pull)

										creds, err := pullAuthorizer.Authorize()
										if err != nil {
											slog.Error("Could not authorize pull", "error", err)
											return cli.Exit(errorMessage(err, "Could not authorize pull"), sysexits.Software)
										}

										dag.
											Container().
											WithRegistryAuth(creds.ServerAddress, creds.Username, dag.SetSecret(creds.ServerAddress, creds.Password)).
											From(pull).
											Export(c.Context, fmt.Sprintf(".deployer/artifacts/images/%s.tar", pull))
									}

									project := dag.Host().Directory(".")

									dockerImage := project.
										DockerBuild().
										WithLabel("org.opencontainers.image.created", time.Now().Format(time.RFC3339))

									for _, label := range labels {
										parts := strings.SplitN(label, "=", 2)

										if len(parts) != 2 {
											slog.Error("Invalid label", "label", label)
										} else {
											dockerImage = dockerImage.WithLabel(parts[0], parts[1])
										}
									}

									_, err = dockerImage.Export(c.Context, outFile)
									if err != nil {
										return cli.Exit(err, 1)
									}

									fmt.Printf("Built image: %s\n", outFile)

									if publish {
										var result *multierror.Error
										creds, err := authorizer.Authorize()
										if err != nil {
											slog.Error("Could not authorize pull", "error", err)
											return cli.Exit(errorMessage(err, "Could not authorize pull"), sysexits.Software)
										}

										imageWithAuth := dockerImage.WithRegistryAuth(creds.ServerAddress, creds.Username, dag.SetSecret(creds.ServerAddress, creds.Password))

										for _, name := range img.Names() {
											addr, err := imageWithAuth.Publish(ctx, name, dagger.ContainerPublishOpts{
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
	dag, err := getDaggerFromContext(ctx)
	if err != nil {
		var err error
		dag, err = dagger.Connect(ctx, dagger.WithLogOutput(daggerLogFile))
		if err != nil {
			return ctx, nil, err
		}

		ctx = context.WithValue(ctx, dagCtxKey, dag)
	}

	return ctx, dag, nil
}

func getDaggerFromContext(ctx context.Context) (*dagger.Client, error) {
	dag, ok := ctx.Value(dagCtxKey).(*dagger.Client)
	if !ok {
		return nil, fmt.Errorf("dagger not found in context")
	}
	return dag, nil
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

func errorMessage(err error, message string) string {
	return fmt.Sprintf("%s: %v", message, err)
}
