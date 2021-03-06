package terraform

import (
	"github.com/hashicorp/terraform/config"
	"github.com/hashicorp/terraform/config/module"
	"github.com/hashicorp/terraform/dag"
)

// RefreshGraphBuilder implements GraphBuilder and is responsible for building
// a graph for refreshing (updating the Terraform state).
//
// The primary difference between this graph and others:
//
//   * Based on the state since it represents the only resources that
//     need to be refreshed.
//
//   * Ignores lifecycle options since no lifecycle events occur here. This
//     simplifies the graph significantly since complex transforms such as
//     create-before-destroy can be completely ignored.
//
type RefreshGraphBuilder struct {
	// Module is the root module for the graph to build.
	Module *module.Tree

	// State is the current state
	State *State

	// Providers is the list of providers supported.
	Providers []string

	// Targets are resources to target
	Targets []string

	// DisableReduce, if true, will not reduce the graph. Great for testing.
	DisableReduce bool

	// Validate will do structural validation of the graph.
	Validate bool
}

// See GraphBuilder
func (b *RefreshGraphBuilder) Build(path []string) (*Graph, error) {
	return (&BasicGraphBuilder{
		Steps:    b.Steps(),
		Validate: b.Validate,
		Name:     "RefreshGraphBuilder",
	}).Build(path)
}

// See GraphBuilder
func (b *RefreshGraphBuilder) Steps() []GraphTransformer {
	// Custom factory for creating providers.
	concreteProvider := func(a *NodeAbstractProvider) dag.Vertex {
		return &NodeApplyableProvider{
			NodeAbstractProvider: a,
		}
	}

	concreteResource := func(a *NodeAbstractResource) dag.Vertex {
		return &NodeRefreshableResource{
			NodeAbstractResource: a,
		}
	}

	concreteDataResource := func(a *NodeAbstractResource) dag.Vertex {
		return &NodeRefreshableDataResource{
			NodeAbstractCountResource: &NodeAbstractCountResource{
				NodeAbstractResource: a,
			},
		}
	}

	steps := []GraphTransformer{
		// Creates all the resources represented in the state
		&StateTransformer{
			Concrete: concreteResource,
			State:    b.State,
		},

		// Creates all the data resources that aren't in the state
		&ConfigTransformer{
			Concrete:   concreteDataResource,
			Module:     b.Module,
			Unique:     true,
			ModeFilter: true,
			Mode:       config.DataResourceMode,
		},

		// Attach the state
		&AttachStateTransformer{State: b.State},

		// Attach the configuration to any resources
		&AttachResourceConfigTransformer{Module: b.Module},

		// Add root variables
		&RootVariableTransformer{Module: b.Module},

		// Create all the providers
		&MissingProviderTransformer{Providers: b.Providers, Concrete: concreteProvider},
		&ProviderTransformer{},
		&DisableProviderTransformer{},
		&ParentProviderTransformer{},
		&AttachProviderConfigTransformer{Module: b.Module},

		// Add the outputs
		&OutputTransformer{Module: b.Module},

		// Add module variables
		&ModuleVariableTransformer{Module: b.Module},

		// Connect so that the references are ready for targeting. We'll
		// have to connect again later for providers and so on.
		&ReferenceTransformer{},

		// Target
		&TargetsTransformer{Targets: b.Targets},

		// Single root
		&RootTransformer{},
	}

	if !b.DisableReduce {
		// Perform the transitive reduction to make our graph a bit
		// more sane if possible (it usually is possible).
		steps = append(steps, &TransitiveReductionTransformer{})
	}

	return steps
}
