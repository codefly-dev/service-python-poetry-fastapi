package main

import (
	"context"
	"embed"
	"github.com/codefly-dev/core/builders"
	v0 "github.com/codefly-dev/core/generated/go/base/v0"

	"github.com/codefly-dev/core/configurations/standards"

	"google.golang.org/grpc/codes"

	"google.golang.org/grpc/status"

	"github.com/codefly-dev/core/templates"

	"github.com/codefly-dev/core/agents"
	"github.com/codefly-dev/core/agents/services"
	"github.com/codefly-dev/core/configurations"
	agentv0 "github.com/codefly-dev/core/generated/go/services/agent/v0"
	"github.com/codefly-dev/core/shared"
)

// Agent version
var agent = shared.Must(configurations.LoadFromFs[configurations.Agent](shared.Embed(info)))

var requirements = builders.NewDependencies(agent.Name,
	builders.NewDependency("service.codefly.yaml"),
	builders.NewDependency(".").WithPathSelect(shared.NewSelect("*.py")),
)

type Settings struct {
	Debug bool `yaml:"debug"` // Developer only

	Watch bool `yaml:"watch"`
}

type Service struct {
	*services.Base

	// Settings
	*Settings
}

func (s *Service) GetAgentInformation(ctx context.Context, _ *agentv0.AgentInformationRequest) (*agentv0.AgentInformation, error) {
	defer s.Wool.Catch()

	readme, err := templates.ApplyTemplateFrom(ctx, shared.Embed(readme), "templates/agent/README.md", s.Information)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &agentv0.AgentInformation{
		RuntimeRequirements: []*agentv0.Runtime{
			{Type: agentv0.Runtime_PYTHON},
		},
		Capabilities: []*agentv0.Capability{
			{Type: agentv0.Capability_BUILDER},
			{Type: agentv0.Capability_RUNTIME},
			{Type: agentv0.Capability_HOT_RELOAD},
		},
		Languages: []*agentv0.Language{
			{Type: agentv0.Language_PYTHON},
		},
		Protocols: []*agentv0.Protocol{
			{Type: agentv0.Protocol_HTTP},
		},
		ReadMe: readme,
	}, nil
}

func NewService() *Service {
	return &Service{
		Base:     services.NewServiceBase(context.Background(), agent),
		Settings: &Settings{},
	}
}

func (s *Service) LoadEndpoints(ctx context.Context, makePublic bool) error {
	defer s.Wool.Catch()
	s.Endpoints = []*v0.Endpoint{}
	for _, endpoint := range s.Configuration.Endpoints {
		endpoint.Application = s.Configuration.Application
		endpoint.Service = s.Configuration.Name
		if makePublic {
			endpoint.Visibility = configurations.VisibilityPublic
		}
		switch endpoint.API {
		case standards.REST:
			rest, err := configurations.NewRestAPIFromOpenAPI(ctx, endpoint, s.Local("swagger/api.swagger.json"))
			if err != nil {
				return s.Wool.Wrapf(err, "cannot create openapi api")
			}
			s.Endpoints = append(s.Endpoints, rest)
			continue
		}
	}
	return nil
}

func main() {
	agents.Register(
		services.NewServiceAgent(agent.Of(configurations.ServiceAgent), NewService()),
		services.NewBuilderAgent(agent.Of(configurations.RuntimeServiceAgent), NewBuilder()),
		services.NewRuntimeAgent(agent.Of(configurations.BuilderServiceAgent), NewRuntime()))
}

//go:embed agent.codefly.yaml
var info embed.FS

//go:embed templates/agent
var readme embed.FS
