package main

import (
	"context"
	"embed"
	"github.com/codefly-dev/core/configurations/standards"
	v0 "github.com/codefly-dev/core/generated/go/base/v0"
	"github.com/codefly-dev/core/runners"
	"github.com/codefly-dev/core/shared"
	"github.com/codefly-dev/core/templates"
	"github.com/codefly-dev/core/wool"

	"github.com/codefly-dev/core/agents/communicate"
	dockerhelpers "github.com/codefly-dev/core/agents/helpers/docker"
	"github.com/codefly-dev/core/agents/services"
	"github.com/codefly-dev/core/configurations"
	agentv0 "github.com/codefly-dev/core/generated/go/services/agent/v0"
	builderv0 "github.com/codefly-dev/core/generated/go/services/builder/v0"
)

type Builder struct {
	*Service
}

func NewBuilder() *Builder {
	return &Builder{
		Service: NewService(),
	}
}
func (s *Builder) Load(ctx context.Context, req *builderv0.LoadRequest) (*builderv0.LoadResponse, error) {
	defer s.Wool.Catch()

	err := s.Base.Builder.Load(ctx, req.Identity, s.Settings)
	if err != nil {
		return nil, err
	}

	requirements.Localize(s.Location)

	s.sourceLocation = s.Local("src")

	err = s.LoadEndpoints(ctx, false)
	if err != nil {
		return s.Builder.LoadError(err)
	}

	// communication on CreateResponse
	err = s.Communication.Register(ctx, communicate.New[builderv0.CreateRequest](createCommunicate()))
	if err != nil {
		return s.Builder.LoadError(err)
	}

	if err != nil {
		return s.Builder.LoadError(err)
	}

	gettingStarted, err := templates.ApplyTemplateFrom(ctx, shared.Embed(factoryFS), "templates/factory/GETTING_STARTED.md", s.Information)
	if err != nil {
		return s.Builder.LoadError(err)
	}
	return s.Builder.LoadResponse(gettingStarted)
}

func (s *Builder) Init(ctx context.Context, req *builderv0.InitRequest) (*builderv0.InitResponse, error) {
	defer s.Wool.Catch()

	s.Wool.Focus("info", wool.Field("info", configurations.MakeProviderInformationSummary(req.ProviderInfos)))

	s.Base.NetworkMappings = req.ProposedNetworkMappings

	for _, info := range req.ProviderInfos {
		s.Base.EnvironmentVariables.Add(configurations.ProviderInformationAsEnvironmentVariables(info)...)
	}

	//
	//hash, err := requirements.Hash(ctx)
	//if err != nil {
	//	return s.Builder.InitError(err)
	//}

	return s.Builder.InitResponse()
}

func (s *Builder) Update(ctx context.Context, req *builderv0.UpdateRequest) (*builderv0.UpdateResponse, error) {
	defer s.Wool.Catch()

	err := s.Base.Templates(nil, services.WithBuilder(builderFS))
	if err != nil {
		return nil, s.Wool.Wrapf(err, "cannot copy and apply template")
	}

	return &builderv0.UpdateResponse{}, nil
}

func (s *Service) GenerateOpenAPI(ctx context.Context) error {
	defer s.Wool.Catch()

	runner, err := runners.NewRunner(ctx, "poetry", "run", "python", "openapi.py")
	if err != nil {
		return s.Wool.Wrapf(err, "cannot create runner")
	}
	runner.WithDir(s.sourceLocation)
	err = runner.Run()
	if err != nil {
		return s.Wool.Wrapf(err, "cannot generate swagger")
	}

	return nil

}

func (s *Builder) Sync(ctx context.Context, req *builderv0.SyncRequest) (*builderv0.SyncResponse, error) {
	defer s.Wool.Catch()

	return s.Builder.SyncResponse()
}

type Env struct {
	Key   string
	Value string
}

type DockerTemplating struct {
	Components      []string
	RuntimePackages []string
	Envs            []Env
}

func (s *Builder) Build(ctx context.Context, req *builderv0.BuildRequest) (*builderv0.BuildResponse, error) {
	defer s.Wool.Catch()

	image := s.DockerImage(req.BuildContext)

	s.Wool.Debug("building docker image", wool.Field("image", image.FullName()))
	ctx = s.WoolAgent.Inject(ctx)

	docker := DockerTemplating{
		Components:      requirements.All(),
		RuntimePackages: s.Settings.RuntimePackages,
	}

	err := shared.DeleteFile(ctx, s.Local("builder/Dockerfile"))
	if err != nil {
		return nil, s.Wool.Wrapf(err, "cannot remove dockerfile")
	}

	err = s.Templates(ctx, docker, services.WithBuilder(builderFS))
	if err != nil {
		return nil, s.Wool.Wrapf(err, "cannot copy and apply template")
	}

	builder, err := dockerhelpers.NewBuilder(dockerhelpers.BuilderConfiguration{
		Root:        s.Location,
		Dockerfile:  "builder/Dockerfile",
		Destination: image,
		Output:      s.Wool,
	})
	if err != nil {
		return nil, s.Wool.Wrapf(err, "cannot create builder")
	}
	_, err = builder.Build(ctx)
	if err != nil {
		return nil, s.Wool.Wrapf(err, "cannot build image")
	}
	return &builderv0.BuildResponse{}, nil
}

type Deployment struct {
	Replicas int
}

func (s *Builder) Deploy(ctx context.Context, req *builderv0.DeploymentRequest) (*builderv0.DeploymentResponse, error) {
	defer s.Wool.Catch()

	image := s.DockerImage(req.BuildContext)

	cfMap, err := services.EnvsAsConfigMapData(s.EnvironmentVariables.Get())
	if err != nil {
		return s.Builder.DeployError(err)
	}

	params := services.DeploymentTemplateInput{
		Image:       image,
		Information: s.Information,
		DeploymentConfiguration: services.DeploymentConfiguration{
			Replicas: 1,
		},
		ConfigMap: cfMap,
	}

	err = s.Builder.Deploy(ctx, req, deploymentFS, params)
	if err != nil {
		return s.Builder.DeployError(err)
	}

	return s.Builder.DeployResponse()
}

const Watch = "with-hot-reload"

func createCommunicate() *communicate.Sequence {
	return communicate.NewSequence(
		communicate.NewConfirm(&agentv0.Message{Name: Watch, Message: "Code hot-reload (Recommended)?", Description: "codefly can restart your service when code changes are detected 🔎"}, true),
	)
}

type CreateConfiguration struct {
	*services.Information
	Image *configurations.DockerImage
	Envs  []string
}

func (s *Builder) Create(ctx context.Context, req *builderv0.CreateRequest) (*builderv0.CreateResponse, error) {
	defer s.Wool.Catch()

	ctx = s.WoolAgent.Inject(ctx)

	session, err := s.Communication.Done(ctx, communicate.Channel[builderv0.CreateRequest]())
	if err != nil {
		return s.Builder.CreateError(err)
	}

	s.Settings.Watch, err = session.Confirm(Watch)
	if err != nil {
		return s.Builder.CreateError(err)
	}

	create := CreateConfiguration{
		Information: s.Information,
		Envs:        []string{},
	}
	err = s.Templates(ctx, create, services.WithFactory(factoryFS))
	if err != nil {
		return s.Base.Builder.CreateError(err)
	}

	runner, err := runners.NewRunner(ctx, "poetry", "install")
	if err != nil {
		return s.Base.Builder.CreateError(err)
	}
	runner.WithDir(s.sourceLocation)

	err = runner.Run()
	if err != nil {
		return s.Base.Builder.CreateError(err)
	}

	err = s.CreateEndpoints(ctx)
	if err != nil {
		return nil, s.Wool.Wrapf(err, "cannot create endpoints")
	}

	return s.Base.Builder.CreateResponse(ctx, s.Settings)
}

func (s *Builder) CreateEndpoints(ctx context.Context) error {
	swagger := s.Local("openapi/api.swagger.json")
	if !shared.FileExists(swagger) {
		err := s.GenerateOpenAPI(ctx)
		if err != nil {
			return s.Wool.Wrapf(err, "cannot generate openapi")
		}

	}
	var err error
	endpoint := s.Configuration.BaseEndpoint(standards.REST)
	s.restEndpoint, err = configurations.NewRestAPIFromOpenAPI(ctx,
		endpoint,
		s.Local("openapi/api.swagger.json"))
	if err != nil {
		return s.Wool.Wrapf(err, "cannot create openapi api")
	}
	s.Endpoints = []*v0.Endpoint{s.restEndpoint}
	return nil
}

//go:embed templates/factory
var factoryFS embed.FS

//go:embed templates/builder
var builderFS embed.FS

//go:embed templates/deployment
var deploymentFS embed.FS
